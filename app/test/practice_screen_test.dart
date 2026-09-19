import 'package:curator/api/client.dart';
import 'package:curator/cases/model.dart';
import 'package:curator/cases/outbox.dart';
import 'package:curator/cases/practice_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Экран разбора.
///
/// В проверках виджетов Flutter подменяет HttpClient своим, и тот отвечает
/// отказом на всё. Поэтому здесь проверяется то, что от сети не зависит:
/// как экран ведёт себя, когда задач не дали, и как показывает разбор,
/// когда задача уже на руках.

class _StubOutbox extends Outbox {
  _StubOutbox() : super(_noApi, MemoryOutboxStore());

  static final _noApi = Api(
    baseUrl: Uri.parse('http://127.0.0.1:1'),
    appKey: 'app-key-1',
    tokens: MemoryTokenStore(),
  );
}

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

    expect(find.text('Больной жалуется на…'), findsOneWidget);
    expect(find.text('Потому что так'), findsNothing);

    await tester.tap(find.text('Первый'));
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
    expect(find.text('Потому что так'), findsOneWidget);
    expect(find.text('Дальше'), findsOneWidget);

    await tester.tap(find.text('Дальше'));
    expect(next, 1);
  });
}
