/// Проверки показа справочника.
///
/// Экраны здесь строятся из готовых данных, а не из базы: настоящая база
/// внутри `testWidgets` не открывается — время подменено, и ожидание
/// ввода-вывода не наступает никогда. Поэтому проверяется то, что и должно
/// проверяться глазами: структура, знаки, отсутствие повторов.
library;

import 'package:curator/reference/criteria.dart';
import 'package:curator/reference/model.dart';
import 'package:curator/reference/source_screen.dart';
import 'package:curator/text/hyphenation.dart';
import 'package:curator/text/prose.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

RefStatement statement({
  required int id,
  required String kind,
  required String body,
  String designation = '',
  String placeRef = '',
}) => RefStatement(
  id: id,
  unitLabel: 'F32',
  kind: kind,
  designation: designation,
  placeRef: placeRef,
  body: body,
  ord: id,
);

Future<void> show(WidgetTester tester, Widget child) => tester.pumpWidget(
  MaterialApp(
    home: Scaffold(body: SingleChildScrollView(child: child)),
  ),
);

void main() {
  group('обозначение', () {
    test('срезается, когда повторено в начале текста', () {
      // Показанное и значком, и первым словом, оно выглядит опечаткой
      // набора: врач видит «G1 G1.».
      expect(
        withoutDesignation('G1. Сниженное настроение', 'G1'),
        'Сниженное настроение',
      );
      expect(withoutDesignation('G1 — длительность', 'G1'), 'длительность');
    });

    test('не срезается из середины и без разделителя', () {
      // «G1» внутри фразы — это ссылка на другой критерий.
      expect(
        withoutDesignation('Дополняет G1 в части срока', 'G1'),
        'Дополняет G1 в части срока',
      );
      // «Гипотимия» начинается с «Г», и срезать её по обозначению «Г»
      // значило бы съесть первую букву критерия.
      expect(withoutDesignation('Гипотимия', 'Г'), 'Гипотимия');
    });
  });

  group('роды положений', () {
    test('идут в порядке появления, а не по алфавиту', () {
      // Источник расставил критерии сам, и переставлять их по названию
      // рода значит читать источник не в том порядке, в каком он написан.
      final groups = byKind([
        statement(id: 1, kind: 'обязательные', body: 'а'),
        statement(id: 2, kind: 'исключения', body: 'б'),
        statement(id: 3, kind: 'обязательные', body: 'в'),
      ]);
      expect(groups.map((g) => g.key), ['обязательные', 'исключения']);
      expect(groups.first.value.length, 2);
    });

    test('незнакомый род получает общий знак, а не теряется', () {
      // Закрытый словарь родов означал бы, что источник со своими словами
      // показать нельзя, — а источник любой.
      expect(statementIcon('чего-то своё'), Icons.article_outlined);
      expect(statementIcon('обязательный'), Icons.check_circle_outline);
      expect(statementIcon('исключение'), Icons.block_outlined);
      // Три рода, которыми зовёт свои критерии МКБ-10, — самый частый из
      // них покрывает 561 блок из 765, и общий знак у него значил бы, что
      // знака нет у большинства критериев справочника.
      expect(statementIcon('клинические описания'), Icons.menu_book_outlined);
      expect(
        statementIcon('диагностические критерии'),
        Icons.check_circle_outline,
      );
      expect(
        statementIcon('дифференциальный диагноз'),
        Icons.compare_arrows_outlined,
      );
    });
  });

  testWidgets('критерии разложены по родам и не повторяют обозначение', (
    tester,
  ) async {
    await show(
      tester,
      CriteriaList(
        statementWord: 'критерий',
        statements: [
          statement(
            id: 1,
            kind: 'обязательные',
            designation: 'G1',
            body: 'G1. Сниженное настроение',
            placeRef: 'с. 112',
          ),
          statement(
            id: 2,
            kind: 'исключения',
            designation: 'G2',
            body: 'G2. Не объясняется соматическим заболеванием',
          ),
        ],
      ),
    );

    expect(find.text('обязательные'), findsOneWidget);
    expect(find.text('исключения'), findsOneWidget);
    expect(find.text('G1'), findsOneWidget);
    expect(find.byIcon(Icons.check_circle_outline), findsOneWidget);
    expect(find.byIcon(Icons.block_outlined), findsOneWidget);
    expect(find.text('с. 112'), findsOneWidget);

    // Обозначение стоит один раз — значком слева, а не ещё и в тексте.
    final texts = tester
        .widgetList<RichText>(find.byType(RichText))
        .map((w) => w.text.toPlainText())
        .toList();
    expect(texts.where((t) => t.contains('G1')).length, 1);
  });

  testWidgets('обозначение-метка показывается с названием и ведёт к нему', (
    tester,
  ) async {
    // «F41.2» само по себе не говорит врачу ничего: чтобы узнать, с чем
    // путают, он должен помнить код наизусть или уйти искать его в дереве,
    // потеряв место, на котором читал.
    var opened = '';
    await show(
      tester,
      CriteriaList(
        statementWord: 'критерий',
        links: const {'F41.2': 'Смешанное тревожное расстройство'},
        onLink: (label) => opened = label,
        statements: [
          statement(
            id: 1,
            kind: 'дифференциальный диагноз',
            designation: 'F41.2',
            body: 'Тревога здесь первична.',
          ),
        ],
      ),
    );

    expect(find.text('F41.2'), findsOneWidget);
    expect(
      find.byWidgetPredicate(
        (w) =>
            w is RichText &&
            w.text.toPlainText().replaceAll(softHyphen, '') ==
                'Смешанное тревожное расстройство',
      ),
      findsOneWidget,
    );

    await tester.tap(find.text('F41.2'));
    expect(opened, 'F41.2');
  });

  testWidgets('обозначение без своей рубрики остаётся обозначением', (
    tester,
  ) async {
    // У приказа обозначение «абз. 2» меткой не является, и ссылкой оно
    // быть не должно: ссылка, ведущая в никуда, хуже её отсутствия.
    await show(
      tester,
      CriteriaList(
        statementWord: 'пункт',
        statements: [
          statement(
            id: 1,
            kind: 'обязательные',
            designation: 'абз. 2',
            body: 'Помощь оказывается при…',
          ),
        ],
      ),
    );
    expect(find.text('абз. 2'), findsOneWidget);
    expect(find.byIcon(Icons.chevron_right), findsNothing);
  });

  testWidgets('безымянный род зовётся словом источника', (tester) async {
    await show(
      tester,
      CriteriaList(
        statementWord: 'критерий',
        statements: [
          statement(id: 1, kind: '', body: 'Первое'),
          statement(id: 2, kind: 'исключения', body: 'Второе'),
        ],
      ),
    );
    // «Положения» у МКБ-10 сказало бы врачу, что приложение не знает, что
    // показывает.
    expect(find.text('Критерии'), findsOneWidget);
  });

  testWidgets('сводка показывает, чего и сколько в рубрике', (tester) async {
    await show(
      tester,
      KindStrip(
        children: 3,
        statements: [
          statement(id: 1, kind: 'обязательные', body: 'а'),
          statement(id: 2, kind: 'обязательные', body: 'б'),
          statement(id: 3, kind: 'длительность', body: 'в'),
        ],
      ),
    );
    expect(find.text('обязательные · 2'), findsOneWidget);
    expect(find.text('длительность · 1'), findsOneWidget);
    expect(find.text('внутри: 3'), findsOneWidget);
    expect(find.byIcon(Icons.schedule_outlined), findsOneWidget);
  });

  testWidgets('группа и запись отличаются знаком, взятым у источника', (
    tester,
  ) async {
    await show(
      tester,
      Column(
        children: [
          UnitRow(
            unit: RefUnit(
              label: 'F30-F39',
              parentLabel: '',
              title: 'Расстройства настроения',
              path: 'F30-F39',
              depth: 0,
              kind: 'group',
              answerable: false,
              statements: 0,
              ord: 0,
            ),
            statementWord: 'критерий',
            onTap: () {},
          ),
          UnitRow(
            unit: RefUnit(
              label: 'F32',
              parentLabel: 'F30-F39',
              title: 'Депрессивный эпизод',
              path: 'F30-F39/F32',
              depth: 1,
              kind: 'entry',
              answerable: true,
              statements: 4,
              ord: 0,
            ),
            statementWord: 'критерий',
            onTap: () {},
          ),
        ],
      ),
    );

    expect(find.byIcon(Icons.folder_outlined), findsOneWidget);
    expect(find.byIcon(Icons.description_outlined), findsOneWidget);
    expect(find.text('Критерии: 4'), findsOneWidget);
  });

  testWidgets('короткая таблица показывается сеткой, длинная — построчно', (
    tester,
  ) async {
    // Сетка проверяется виджетом, а не только выбором вида: выбор может
    // быть верным, а нарисованное — пустым, и разницу видно только здесь.
    await show(
      tester,
      const ProseTable(
        heads: true,
        rows: [
          ['Код', 'Синдром'],
          ['F10.2', 'Синдром зависимости'],
        ],
      ),
    );
    expect(find.byType(Table), findsOneWidget);
    expect(find.text('Синдром зависимости'), findsOneWidget);

    // Длинные ячейки в сетку не идут: шапка там становится подписью над
    // значением, и каждая строка читается сверху вниз.
    const long =
        'Паркинсонизм предшествует деменции или совпадает с ней по времени, '
        'и двигательный синдром при этом ведущий';
    await show(
      tester,
      const ProseTable(
        heads: true,
        rows: [
          ['Признак', 'F02.3'],
          ['Двигательный синдром', long],
        ],
      ),
    );
    expect(find.byType(Table), findsNothing);
    // Шапка показана подписью, а не потеряна вместе с сеткой.
    expect(find.text('F02.3'), findsOneWidget);
    expect(find.text('Признак'), findsOneWidget);
  });

  testWidgets('выноска и линейка не теряются при показе', (tester) async {
    await show(
      tester,
      const Prose(
        'Первое.\n\n> **Примечание:** особый случай.\n\n---\n\nВторое.',
      ),
    );
    expect(find.byType(Divider), findsOneWidget);
    final texts = tester
        .widgetList<RichText>(find.byType(RichText))
        .map((w) => w.text.toPlainText().replaceAll(softHyphen, ''))
        .toList();
    expect(texts.any((t) => t.contains('Примечание: особый случай.')), isTrue);
    expect(texts.any((t) => t.contains('*')), isFalse);
  });

  test('путь показывается без последнего звена', () {
    // Иначе над меткой «F32.1» стояло бы «F30-F39 › F32 › F32.1», то есть
    // сама рубрика дважды.
    expect(breadcrumb('F30-F39/F32/F32.1'), 'F30-F39 › F32');
    expect(breadcrumb('F30-F39'), '');
  });

  testWidgets('длинные слова показываются с переносами', (tester) async {
    // Переносы — свойство показа: в самой строке их нет, иначе поиск и
    // копирование получали бы слово разрезанным.
    await show(
      tester,
      CriteriaList(
        statementWord: 'критерий',
        statements: [
          statement(id: 1, kind: '', body: 'Дифференциальный диагноз'),
        ],
      ),
    );
    final text = tester
        .widgetList<RichText>(find.byType(RichText))
        .map((w) => w.text.toPlainText())
        .join(' ');
    expect(text, contains(softHyphen));
    expect(text.replaceAll(softHyphen, ''), contains('Дифференциальный'));
  });
}
