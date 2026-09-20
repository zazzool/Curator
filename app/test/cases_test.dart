import 'dart:convert';

import 'package:curator/api/client.dart';
import 'package:curator/cases/feed.dart';
import 'package:curator/cases/model.dart';
import 'package:curator/cases/outbox.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_server.dart';

/// Разбор задачи, лента и очередь разборов.
///
/// Главное здесь — что непонятое не применяется, а понятое не теряется.
/// Задача, разобранная наполовину, выглядит на экране рабочей, и врач
/// отвечает на то, чего ему не показали; очередь, потерянная на обрыве, —
/// это вечер работы, о пропаже которого он узнает по несошедшемуся уровню.

Map<String, dynamic> _row({
  String id = 'c-1',
  String kind = 'recognise',
  String answer = 'А',
  List<Map<String, dynamic>>? segments,
  List<Map<String, dynamic>>? options,
}) => {
  'id': id,
  'unitLabel': 'F20.0',
  'sourceId': 1,
  'body': {
    'title': 'Задача',
    'kind': kind,
    'answer': answer,
    'explanationMd': 'Разбор',
    'difficulty': 3,
    'segments':
        segments ??
        [
          {
            'text': 'Больной жалуется на…',
            'statements': ['абз. 1'],
          },
        ],
    'options':
        options ??
        [
          {'label': 'А', 'text': 'Первый'},
          {'label': 'Б', 'text': 'Второй'},
        ],
  },
};

void main() {
  group('время над задачей', () {
    test('ночь над задачей не уезжает девятичасовым ответом', () {
      // Промежуток считается от показа до ответа и включает свёрнутое
      // приложение, уснувший телефон и ночь между ними. Врач,
      // вернувшийся к задаче наутро, записывал девятичасовой ответ, и он
      // уезжал в сводку решаемости наравне с настоящими.
      const overnight = 9 * 60 * 60 * 1000;
      expect(PendingAttempt.clampSpent(overnight), PendingAttempt.maxSpentMs);
    });

    test('переведённые назад часы дают ноль, а не отрицательное', () {
      // Разность двух «сейчас» бывает меньше нуля: часы устройства
      // переводят назад. Сводка решаемости считается по положительным, и
      // отрицательное молча выпадало из выборки.
      expect(PendingAttempt.clampSpent(-5000), 0);
    });

    test('обычный ответ не трогается', () {
      expect(PendingAttempt.clampSpent(12000), 12000);
    });
  });

  group('разбор задачи', () {
    test('целая задача разбирается', () {
      final one = CaseItem.tryParse(_row())!;
      expect(one.id, 'c-1');
      expect(one.kind, CaseKind.recognise);
      expect(one.segments.single.statements, ['абз. 1']);
      expect(one.options.length, 2);
      expect(one.unitLabel, 'F20.0');
      expect(one.isCorrect(one.options.first), isTrue);
      expect(one.isCorrect(one.options.last), isFalse);
    });

    test('у задачи-действия сверяется текст, а не метка', () {
      // Сверь мы здесь одно, а пошли на сервер другое — и решаемость
      // задачи считалась бы не по тому, что видел врач.
      final one = CaseItem.tryParse(
        _row(
          kind: 'action',
          answer: 'Отменить препарат',
          options: [
            {'text': 'Отменить препарат'},
            {'text': 'Удвоить дозу'},
          ],
        ),
      )!;
      expect(one.kind, CaseKind.action);
      expect(one.chosenValue(one.options.first), 'Отменить препарат');
      expect(one.isCorrect(one.options.first), isTrue);
    });

    test('задача без верного ответа среди вариантов отбрасывается', () {
      // Она не решается никем и никогда, и это не сложность, а поломка.
      expect(CaseItem.tryParse(_row(answer: 'Я')), isNull);
    });

    test('задача с одним вариантом отбрасывается', () {
      // Один вариант — не выбор.
      expect(
        CaseItem.tryParse(
          _row(
            options: [
              {'label': 'А', 'text': 'Единственный'},
            ],
          ),
        ),
        isNull,
      );
    });

    test('задача без условия отбрасывается', () {
      expect(CaseItem.tryParse(_row(segments: [])), isNull);
    });

    test('задача незнакомого вида отбрасывается', () {
      // Словарь закрыт: вид «на всякий случай» означал бы экран, не
      // умеющий ни спросить, ни проверить.
      expect(CaseItem.tryParse(_row(kind: 'выдуманный')), isNull);
    });

    test('лишнее поле не мешает разбору', () {
      // Формат растёт добавлением, и на руках у врачей стоят сборки,
      // которые обновятся не завтра.
      final row = _row();
      (row['body'] as Map<String, dynamic>)['ещёНеЗнакомое'] = {'что': 'то'};
      expect(CaseItem.tryParse(row), isNotNull);
    });
  });

  group('лента', () {
    late FakeServer server;
    late Api api;

    setUp(() async {
      server = await FakeServer.start();
      final tokens = MemoryTokenStore();
      await tokens.write('t-42');
      api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
    });

    tearDown(() async {
      api.close();
      await server.stop();
    });

    test('битая задача выбрасывается, а страница доезжает', () async {
      // Одна битая задача не должна стоить врачу всей ленты, но и молчать
      // о ней нельзя: выброшенная половина выглядит как «задач больше
      // нет».
      server.replies.add(
        Reply(200, {
          'cases': [_row(), _row(id: 'c-2', answer: 'Я'), _row(id: 'c-3')],
          'next': 'c-3',
          'version': 7,
        }),
      );

      final page = await Feed(api).page(limit: 3);
      expect(page.cases.map((one) => one.id), ['c-1', 'c-3']);
      expect(page.dropped, 1);
      expect(page.next, 'c-3');
      expect(page.version, 7);
    });

    test('дочитанная лента — исправный случай', () async {
      // Пустой список, а не отказ: null уронил бы приложение именно здесь.
      server.replies.add(Reply(200, {'cases': [], 'next': '', 'version': 7}));
      final page = await Feed(api).page();
      expect(page.cases, isEmpty);
      expect(page.next, isEmpty);
    });
  });

  group('очередь разборов', () {
    late FakeServer server;
    late Api api;
    late Outbox outbox;

    setUp(() async {
      server = await FakeServer.start();
      final tokens = MemoryTokenStore();
      await tokens.write('t-42');
      api = Api(baseUrl: server.origin, appKey: 'app-key-1', tokens: tokens);
      outbox = Outbox(api, MemoryOutboxStore());
    });

    tearDown(() async {
      api.close();
      await server.stop();
    });

    PendingAttempt attempt(String id) => PendingAttempt(
      caseId: id,
      correct: true,
      answer: 'А',
      mode: '',
      spentMs: 4200,
      idemKey: 'k-$id',
      happenedAt: DateTime.utc(2026, 9, 19, 8),
    );

    test('накопленное уходит одной пачкой', () async {
      await outbox.add(attempt('c-1'));
      await outbox.add(attempt('c-2'));
      server.replies.add(Reply(200, {'accepted': 2}));

      final out = await outbox.flush();
      expect(out.sent, 2);
      expect(out.left, 0);
      expect(await outbox.pending(), 0);

      final sent = jsonDecode(server.taken.single.body) as Map<String, dynamic>;
      final attempts = sent['attempts'] as List;
      expect(attempts.length, 2);
      // Время устройства, а не доставки: врач разбирал задачи в метро, а
      // посылка ушла вечером.
      expect(attempts.first['happenedAt'], '2026-09-19T08:00:00.000Z');
      expect(attempts.first['idemKey'], 'k-c-1');
    });

    test('отказ не съедает очередь', () async {
      // Очистить её до ответа значит отдать врачу отказ и забрать вечер.
      await outbox.add(attempt('c-1'));
      server.replies.add(Reply(500, {'error': 'Разборы не сохранились'}));

      final out = await outbox.flush();
      expect(out.sent, 0);
      expect(out.failure, isNotNull);
      expect(await outbox.pending(), 1);
    });

    test('повторная отправка безопасна', () async {
      // Ключ повторности у каждого разбора свой, и второй раз он не ляжет.
      await outbox.add(attempt('c-1'));
      server.replies.add(Reply(500, {}));
      await outbox.flush();

      server.replies.add(Reply(200, {'accepted': 0, 'repeated': 1}));
      final out = await outbox.flush();
      expect(out.sent, 1);
      expect(await outbox.pending(), 0);
    });

    test('ответ, данный во время отправки, не пропадает', () async {
      // Тот самый случай, ради которого правки очереди выстроены в
      // очередь. Прежде flush снимал список, уходил в сеть, а вернувшись
      // — записывал поверх очереди пустоту: ответ, данный врачом за эти
      // секунды, исчезал молча, и счётчик «ждут отправки» показывал ноль.
      final store = MemoryOutboxStore();
      final box = Outbox(api, store);
      await box.add(attempt('c-1'));
      server.replies.add(Reply(200, {'accepted': 1}));

      final flushing = box.flush();
      await box.add(attempt('c-2'));
      final out = await flushing;

      expect(out.sent, 1);
      expect(out.left, 1, reason: 'второй ответ обязан остаться в очереди');
      final left = await store.read();
      expect(left.length, 1);
      expect(left.single['idemKey'], 'k-c-2');
    });

    test('негодная строка не уезжает и не уносит очередь', () async {
      final store = MemoryOutboxStore();
      await store.write([
        {'caseId': '', 'idemKey': 'k-1'},
        attempt('c-2').toJson(),
      ]);
      server.replies.add(Reply(200, {'accepted': 1}));

      final out = await Outbox(api, store).flush();
      expect(out.sent, 1);
      final sent = jsonDecode(server.taken.single.body) as Map<String, dynamic>;
      expect((sent['attempts'] as List).length, 1);
    });
  });
}
