import 'dart:io';

import 'package:curator/api/client.dart';
import 'package:curator/cases/feed.dart';
import 'package:curator/cases/model.dart';
import 'package:curator/cases/outbox.dart';
import 'package:curator/cases/practice_screen.dart';
import 'package:curator/text/hyphenation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Экран разбора.
///
/// В проверках виджетов Flutter подменяет HttpClient своим, и тот отвечает
/// отказом на всё. Поэтому здесь проверяется то, что от сети не зависит:
/// как экран ведёт себя, когда задач не дали, и как показывает разбор,
/// когда задача уже на руках.

class _StubOutbox extends Outbox {
  _StubOutbox._(this.store) : super(_noApi, store);

  factory _StubOutbox() => _StubOutbox._(MemoryOutboxStore());

  /// Хранилище открыто проверке намеренно: ответ врача, положенный в
  /// очередь, — это и есть главное действие приложения, и проверять его
  /// по экрану значит не проверять его вовсе.
  final MemoryOutboxStore store;

  static final _noApi = Api(
    baseUrl: Uri.parse('http://127.0.0.1:1'),
    appKey: 'app-key-1',
    tokens: MemoryTokenStore(),
  );
}

/// Очередь, в которую нельзя записать: на устройстве кончилось место.
class _FullDiskOutbox extends Outbox {
  _FullDiskOutbox() : super(_StubOutbox._noApi, MemoryOutboxStore());

  @override
  Future<void> add(PendingAttempt attempt) async {
    throw const FileSystemException('нет места на устройстве');
  }
}

/// Лента с одной задачей.
///
/// Подменяется целиком: проверка виджетов до сокета не достучится —
/// Flutter подменяет `HttpClient` и отвечает 400 на всё, — и без этого
/// шва экран разбора проверялся бы только там, где задач ему не дали.
class _OneCaseFeed extends Feed {
  _OneCaseFeed(this.one) : super(_StubOutbox._noApi);

  final CaseItem one;

  @override
  Future<FeedPage> page({
    String after = '',
    String path = '',
    int limit = 0,
  }) async {
    return FeedPage(cases: [one], next: '', version: 1, dropped: 0);
  }
}

/// Задача, на которой проверяется ответ.
CaseItem theCase() {
  return CaseItem.tryParse({
    'id': 'c-7',
    'unitLabel': 'F20.0',
    'body': {
      'title': 'Задача',
      'kind': 'recognise',
      'answer': 'Б',
      'explanationMd': 'Потому что так',
      'segments': [
        {'text': 'Больной жалуется на…'},
      ],
      'options': [
        {'label': 'А', 'text': 'Первый'},
        {'label': 'Б', 'text': 'Второй'},
      ],
    },
  })!;
}

/// Ищет текст, показанный размеченным.
///
/// `find.text` смотрит только на `Text.data`, а условие, варианты и разбор
/// показываются `Text.rich` — с разобранной разметкой и расставленными
/// мягкими переносами. Переносы при сверке снимаются: они свойство показа,
/// и проверять их здесь значило бы проверять раскладку.
Finder prose(String text) => find.byWidgetPredicate(
  (w) =>
      w is RichText && w.text.toPlainText().replaceAll(softHyphen, '') == text,
);

void main() {
  testWidgets('без сети экран говорит словами и даёт повторить', (
    tester,
  ) async {
    final api = Api(
      baseUrl: Uri.parse('http://127.0.0.1:1'),
      appKey: 'app-key-1',
      tokens: MemoryTokenStore(),
    );
    addTearDown(api.close);

    await tester.pumpWidget(
      MaterialApp(
        home: PracticeScreen(api: api, outbox: _StubOutbox()),
      ),
    );
    await tester.pumpAndSettle();

    // Экран, молча оставшийся пустым, врач читает как поломку приложения.
    expect(find.text('Ещё раз'), findsOneWidget);
  });

  testWidgets('ответ врача ложится в очередь целиком', (tester) async {
    // Главное действие приложения, и проверено оно не было ничем: обе
    // прежние проверки проходили с ПУСТЫМ телом `_answer`. Ключ
    // повторности, правильность, метка варианта и время над задачей
    // уезжают на сервер именно отсюда — и уезжали бы пустыми молча.
    final one = theCase();
    final outbox = _StubOutbox();
    final api = Api(
      baseUrl: Uri.parse('http://127.0.0.1:1'),
      appKey: 'app-key-1',
      tokens: MemoryTokenStore(),
    );
    addTearDown(api.close);

    final screen = PracticeScreen(
      api: api,
      outbox: outbox,
      feed: _OneCaseFeed(one),
    );
    await tester.pumpWidget(MaterialApp(home: screen));
    await tester.pumpAndSettle();

    await tester.tap(prose('Второй'));
    await tester.pumpAndSettle();

    final rows = await outbox.store.read();
    expect(rows, hasLength(1));
    final put = PendingAttempt.tryParse(rows.single)!;
    expect(put.caseId, 'c-7');
    expect(put.correct, isTrue);
    // Ответ сверяется с МЕТКОЙ варианта, а не с его текстом: задача с
    // пустыми метками разбирается в null целиком, и подмени здесь метку
    // текстом — сервер перестал бы засчитывать верные ответы.
    expect(put.answer, 'Б');
    expect(put.idemKey, isNotEmpty);
    expect(put.spentMs, lessThanOrEqualTo(PendingAttempt.maxSpentMs));
  });

  testWidgets('разбор, не записавшийся на устройство, не пропадает молча', (
    tester,
  ) async {
    // Самое дорогое из найденного аудитом в приложении: `outbox.add`
    // бросал на переполненном диске, отказ уходил необработанным, разбор
    // исчезал, а кнопка «Дальше» появлялась как обычно — и врач
    // продолжал заниматься впустую.
    final one = theCase();

    final screen = PracticeScreen(
      api: _StubOutbox._noApi,
      outbox: _FullDiskOutbox(),
      feed: _OneCaseFeed(one),
    );
    await tester.pumpWidget(MaterialApp(home: screen));
    await tester.pumpAndSettle();

    await tester.tap(prose('Второй'));
    await tester.pumpAndSettle();

    expect(find.textContaining('не записался'), findsOneWidget);
    expect(find.text('Записать ещё раз'), findsOneWidget);
  });

  testWidgets('ответ показывает разбор и верный вариант', (tester) async {
    final one = CaseItem.tryParse({
      'id': 'c-1',
      'unitLabel': 'F20.0',
      'body': {
        'title': 'Задача',
        'kind': 'recognise',
        'answer': 'Б',
        'explanationMd': 'Потому что так',
        'segments': [
          {'text': 'Больной жалуется на…'},
        ],
        'options': [
          {'label': 'А', 'text': 'Первый'},
          {'label': 'Б', 'text': 'Второй'},
        ],
      },
    })!;

    Option? chosen;
    var next = 0;
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CaseView(
            one: one,
            chosen: chosen,
            onChoose: (option) => chosen = option,
            onNext: () => next++,
          ),
        ),
      ),
    );

    expect(prose('Больной жалуется на…'), findsOneWidget);
    expect(prose('Потому что так'), findsNothing);

    await tester.tap(prose('Первый'));
    expect(chosen, isNotNull);

    // Разбор показывается после ответа, и верный вариант виден всегда:
    // врач, ответивший неверно, должен увидеть, как было правильно, здесь
    // же — иначе он уйдёт с экрана, так и не узнав.
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CaseView(
            one: one,
            chosen: one.options.first,
            onChoose: (_) {},
            onNext: () => next++,
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('Неверно'), findsOneWidget);
    expect(prose('Потому что так'), findsOneWidget);
    expect(find.text('Дальше'), findsOneWidget);

    await tester.tap(find.text('Дальше'));
    expect(next, 1);
  });

  testWidgets('исход варианта сказан словами, а не одним цветом', (
    tester,
  ) async {
    // Довод в коде про цвет («цвет различают не все») был исполнен
    // наполовину: рядом с вариантом рисовалась галочка или крестик, но
    // голая Icon в дерево доступности не попадает вовсе. Незрячему врачу
    // не говорилось ни «верно», ни «неверно» — после ответа вариант
    // читался так же, как до него, и задача не разбиралась в принципе.

    // Ручка дерева доступности гасится в теле проверки, а не через
    // addTearDown: Flutter сверяет погашенные ручки раньше, чем зовёт
    // уборку за проверкой, и отложенное гашение роняет проверку уже
    // после того, как все сверки прошли.
    final handle = tester.ensureSemantics();

    final one = CaseItem.tryParse({
      'id': 'c-2',
      'unitLabel': 'F20.0',
      'body': {
        'title': 'Задача',
        'kind': 'recognise',
        'answer': 'Б',
        'explanationMd': 'Потому что так',
        'segments': [
          {'text': 'Больной жалуется на…'},
        ],
        'options': [
          {'label': 'А', 'text': 'Первый'},
          {'label': 'Б', 'text': 'Второй'},
        ],
      },
    })!;

    // До ответа исхода нет ни у кого: сказанный заранее, он и есть ответ.
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CaseView(
            one: one,
            chosen: null,
            onChoose: (_) {},
            onNext: () {},
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel(RegExp('верно')), findsNothing);

    // Ответили первым — он неверный, второй верный. Сказать надо про оба:
    // врач, ответивший неверно, узнаёт правильный ответ здесь же.
    await tester.pumpWidget(
      MaterialApp(
        home: Scaffold(
          body: CaseView(
            one: one,
            chosen: one.options.first,
            onChoose: (_) {},
            onNext: () {},
          ),
        ),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.bySemanticsLabel(RegExp(r'^неверно\.')), findsOneWidget);
    expect(find.bySemanticsLabel(RegExp(r'^верно\.')), findsOneWidget);

    handle.dispose();
  });
}
