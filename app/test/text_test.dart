/// Проверки показа текста: переносы, разметка, слова и числа.
///
/// Тексты справочника и условий задач — это то, ради чего приложение
/// ставят, и ломаются они молча: перенос не по правилу выглядит опечаткой
/// набора, а незакрытая звёздочка съедает абзац целиком. Ни то, ни другое
/// не роняет сборку и не попадает ни в один журнал.
library;

import 'package:curator/text/hyphenation.dart';
import 'package:curator/text/plural.dart';
import 'package:curator/text/prose.dart';
import 'package:flutter_test/flutter_test.dart';

/// Слово с переносами в читаемом виде.
String shown(String word) => hyphenateWord(word).replaceAll(softHyphen, '-');

void main() {
  group('переносы', () {
    test('делят слово по слогам', () {
      expect(shown('дифференциальный'), 'диф-фе-рен-ци-аль-ный');
      expect(shown('критерий'), 'кри-те-рий');
      expect(shown('длительность'), 'дли-тель-ность');
    });

    test('не отрывают «ь», «ъ» и «й» от предшествующей буквы', () {
      // «подъ-езд», а не «под-ъезд»: перенос ПОСЛЕ них разрешён, перед —
      // нет, и это самая заметная глазу ошибка переноса.
      expect(shown('подъезд'), 'подъ-езд');
      for (final word in ['объявление', 'большинство', 'майонез']) {
        final parts = shown(word).split('-');
        for (final part in parts) {
          expect(
            'ьъй'.contains(part[0]),
            isFalse,
            reason: 'кусок «$part» слова «$word» начинается с ь, ъ или й',
          );
        }
      }
    });

    test('не оставляют и не переносят одну букву', () {
      // Мерка про края слова, а не про каждый кусок: перенос — это
      // разрешённое место, и «чи-та-е-мый» значит лишь, что рвать можно и
      // до одинокой гласной, и после неё.
      for (final word in [
        'утомляемость',
        'аденома',
        'эпизодический',
        'учение',
      ]) {
        final parts = shown(word).split('-');
        expect(parts.first.length, greaterThanOrEqualTo(2), reason: word);
        expect(parts.last.length, greaterThanOrEqualTo(2), reason: word);
      }
    });

    test('делят группу согласных, а не уносят её целиком', () {
      // Правило на каждую длину группы здесь уже стояло, и четвёртой длины
      // в нём не оказалось: «большинство» переносилось как «боль-шинство».
      expect(shown('большинство'), 'боль-шин-ство');
      expect(shown('расстройство'), 'рас-строй-ство');
      // Двойная согласная делится между собой: «рас-строй», а не
      // «ра-сстрой».
      expect(shown('депрессивный'), 'деп-рес-сив-ный');
    });

    test('не трогают короткое и нерусское', () {
      expect(shown('отёк'), 'отёк');
      expect(shown('F32.1'), 'F32.1');
      expect(hyphenateWord('depressive'), 'depressive');
    });

    test('сохраняют слово целиком', () {
      // Мягкий перенос — это знак «здесь можно», а не разрез: убери его, и
      // должно остаться ровно то, что пришло. Иначе поиск по справочнику
      // перестал бы находить перенесённые слова.
      for (final word in ['психопатологический', 'настроения', 'подъезд']) {
        expect(hyphenateWord(word).replaceAll(softHyphen, ''), word);
      }
    });

    test('в тексте не трогают знаки и пробелы', () {
      const text = 'Длительность — не менее 2 недель (F32.1).';
      expect(hyphenate(text).replaceAll(softHyphen, ''), text);
    });
  });

  group('разметка', () {
    test('разбирает абзацы, списки и подзаголовки', () {
      final blocks = parseProse('''
## Обязательные

- первый признак
- второй признак

1. сначала это
2. потом то

Обычный абзац,
разложенный по строкам.
''');
      expect(blocks.map((b) => b.kind).toList(), [
        ProseKind.heading,
        ProseKind.bullet,
        ProseKind.bullet,
        ProseKind.numbered,
        ProseKind.numbered,
        ProseKind.paragraph,
      ]);
      // Номер берётся из источника, а не из счётчика показа: критерий «2.»
      // обязан остаться вторым, даже если показан первым.
      expect(blocks[4].marker, '2');
      // Мягкий перенос строки внутри абзаца склеивается пробелом: в файле
      // абзац разложен по ширине файла, а на экране — по ширине экрана.
      expect(blocks[5].text, 'Обычный абзац, разложенный по строкам.');
    });

    test('непонятое остаётся текстом, а не исчезает', () {
      final blocks = parseProse('| таблица | не поддержана |');
      expect(blocks.single.kind, ProseKind.paragraph);
      expect(blocks.single.text, contains('таблица'));
    });

    test('таблица разбирается сеткой, а не абзацем с чертами', () {
      // 182 текста критериев из 765 содержат таблицу. Показанная абзацем,
      // она даёт «| F10.0 | Острая интоксикация |» посреди страницы.
      final blocks = parseProse('''
| Код | Синдром |
|-----|---------|
| F10.0 | Острая интоксикация |
| F10.2 | Синдром зависимости |
''');
      expect(blocks.single.kind, ProseKind.table);
      expect(blocks.single.heads, isTrue);
      expect(blocks.single.rows.length, 3);
      expect(blocks.single.rows.first, ['Код', 'Синдром']);
      expect(blocks.single.rows[2][1], 'Синдром зависимости');
    });

    test('строка-разделитель шапки не становится строкой данных', () {
      // Иначе в таблице появляется строка «--- | ---», то есть разметка,
      // показанная как содержимое.
      final blocks = parseProse('| а | б |\n|:--|--:|\n| 1 | 2 |');
      expect(blocks.single.rows.length, 2);
      for (final row in blocks.single.rows) {
        for (final cell in row) {
          expect(cell, isNot(contains('--')));
        }
      }
    });

    test('таблица кончается там, где кончаются её строки', () {
      // Абзац под таблицей, приклеенный к ней, уехал бы в последнюю
      // ячейку.
      final blocks = parseProse('| а | б |\n| 1 | 2 |\nПосле таблицы.');
      expect(blocks.map((b) => b.kind).toList(), [
        ProseKind.table,
        ProseKind.paragraph,
      ]);
      expect(blocks[1].text, 'После таблицы.');
    });

    test('сетка выбирается по замеру, а не всегда', () {
      // Три колонки по двести знаков в сетке дают столбик слов вместо
      // сравнения, и каждая строка высотой в экран.
      expect(
        ProseTable.asGrid([
          ['Код', 'Синдром'],
          ['F10.2', 'Синдром зависимости'],
        ]),
        isTrue,
      );
      expect(
        ProseTable.asGrid([
          ['Признак', 'F00', 'F02.3'],
          [
            'Двигательный синдром',
            'Отсутствует в начале; появляется поздно',
            'Паркинсонизм предшествует или совпадает с деменцией',
          ],
        ]),
        isFalse,
      );
    });

    test('выноска отделяется от абзаца, а не приклеивается к нему', () {
      // «> Примечание:» встречается в 446 строках критериев, и слипшись с
      // соседним абзацем оно читается как его продолжение.
      final blocks = parseProse(
        'Обычный абзац.\n> **Примечание:** особый случай.\n> Продолжение.\n'
        'Следующий абзац.',
      );
      expect(blocks.map((b) => b.kind).toList(), [
        ProseKind.paragraph,
        ProseKind.quote,
        ProseKind.paragraph,
      ]);
      expect(blocks[1].text, '**Примечание:** особый случай. Продолжение.');
    });

    test('линейка не пропадает и не съедает соседей', () {
      final blocks = parseProse('Первое\n\n---\n\nВторое');
      expect(blocks.map((b) => b.kind).toList(), [
        ProseKind.paragraph,
        ProseKind.rule,
        ProseKind.paragraph,
      ]);
    });

    test('незакрытое выделение не съедает остаток', () {
      // Звёздочка посреди фразы встречается как знак сноски, и принять её
      // за начало курсива значит потерять конец абзаца.
      final spans = proseSpans('тревога * и страх', base: null, hyphens: false);
      final text = spans.map((s) => s.toPlainText()).join();
      expect(text, 'тревога * и страх');
    });

    test('выделение разбирается и не остаётся звёздочками', () {
      final spans = proseSpans(
        'не менее **двух** недель',
        base: null,
        hyphens: false,
      );
      expect(spans.map((s) => s.toPlainText()).join(), 'не менее двух недель');
      expect(spans.length, greaterThan(1));
    });
  });

  group('слова и числа', () {
    test('множественное берётся по окончанию', () {
      expect(pluralWord('критерий'), 'Критерии');
      expect(pluralWord('положение'), 'Положения');
      expect(pluralWord('рубрика'), 'Рубрики');
      expect(pluralWord('пункт'), 'Пункты');
      expect(pluralWord('признак'), 'Признаки');
      expect(pluralWord('степень'), 'Степени');
    });

    test('число разбито на разряды неразрывным пробелом', () {
      // Обычный пробел раскладка разорвала бы переносом строки, и число
      // разъехалось бы на две строки.
      expect(grouped(3210), '3\u202F210');
      expect(grouped(940), '940');
      expect(counted(12, 'рубрика'), 'Рубрики: 12');
    });
  });
}
