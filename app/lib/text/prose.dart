/// Показ текста: структура вместо сплошного абзаца.
///
/// # Зачем разбирать разметку на устройстве
///
/// Критерии и условия задач лежат разметкой — списками, выделениями,
/// подзаголовками. Показанные одним `Text`, они превращаются в стену слов
/// со звёздочками посреди строк: врач читает не текст, а его исходник.
/// Поэтому разметка разбирается и показывается тем, чем она является:
/// список — списком, выделение — начертанием.
///
/// # Подмножество, а не Markdown целиком
///
/// Разбирается ровно то, что встречается в источниках: абзацы, маркерные
/// и нумерованные списки, подзаголовки, выноски, линейки, таблицы,
/// полужирное, курсив и моноширинное. Полный Markdown — это зависимость, а
/// зависимость здесь называется вслух вместе с тем, чего без неё нельзя.
///
/// Здесь было написано, что таблиц в критериях не встречается, и на этом
/// основании их не разбирали. Довод оказался неверным — он был сделан до
/// того, как появился материал: в критериях МКБ-10 таблица стоит в 182
/// текстах из 765, а выноска «> Примечание:» — в 446 строках. Показанная
/// абзацем таблица — это и есть та «стена слов со звёздочками», ради
/// которой писался весь этот разбор, только хуже: в ней ещё и вертикальные
/// черты между словами.
///
/// # Таблица на телефоне — не всегда таблица
///
/// Столбец шириной в телефон не вмещает трёх колонок по двести знаков, а
/// боковая прокрутка прячет то, ради чего таблицу и читают, — сравнение.
/// Поэтому вид выбирается по замеру самой таблицы: короткие ячейки
/// показываются сеткой, длинные — построчно, где шапка становится
/// подписью над значением. Порог замерен по материалу, а не выбран на глаз.
///
/// # Непонятое показывается как текст, а не теряется
///
/// Строка, не подошедшая ни под одно правило, становится обычным абзацем.
/// Это то же правило, что везде: непонятое не применяется, но и не роняет
/// остального — и уж точно не исчезает с экрана.
library;

import 'package:flutter/material.dart';

import 'hyphenation.dart';

/// Род блока.
enum ProseKind {
  /// Обычный абзац.
  paragraph,

  /// Пункт маркерного списка.
  bullet,

  /// Пункт нумерованного списка. Номер источника сохраняется в [ProseBlock.marker]:
  /// критерий «3.» обязан остаться третьим, даже если показан вторым по счёту.
  numbered,

  /// Подзаголовок внутри текста.
  heading,

  /// Выноска: «> Примечание: …». Это не цитата чужого текста, а замечание
  /// самого источника, и отделяется оно затем же, зачем отделено в нём.
  quote,

  /// Разделительная линейка между частями текста.
  rule,

  /// Таблица. Ячейки лежат в [ProseBlock.rows], текста у неё нет.
  table,
}

/// Блок разобранного текста.
class ProseBlock {
  const ProseBlock({
    required this.kind,
    required this.text,
    this.level = 0,
    this.marker = '',
    this.rows = const [],
    this.heads = false,
  });

  final ProseKind kind;

  /// Текст блока с внутренней разметкой — она разбирается при показе.
  final String text;

  /// Вложенность списка или глубина подзаголовка.
  final int level;

  /// Номер пункта, как он записан в источнике.
  final String marker;

  /// Строки таблицы, ячейка за ячейкой. Первая строка — шапка, если под
  /// ней в источнике стояла строка-разделитель.
  final List<List<String>> rows;

  /// Есть ли у таблицы шапка. У таблицы без разделителя первая строка —
  /// такие же данные, и набрать её жирным значило бы соврать.
  final bool heads;
}

final _bullet = RegExp(r'^(\s*)[-*•]\s+(.*)$');
final _numbered = RegExp(r'^(\s*)(\d+)[.)]\s+(.*)$');
final _heading = RegExp(r'^(#{1,6})\s+(.*)$');
final _quote = RegExp(r'^\s*>\s?(.*)$');
final _rule = RegExp(r'^\s*(-{3,}|\*{3,}|_{3,})\s*$');
final _row = RegExp(r'^\s*\|(.*)\|?\s*$');
// Строка-разделитель шапки: «|---|:--:|». Она не данные, и показать её
// четвёртой строкой таблицы значило бы показать разметку вместо текста.
final _rowBreak = RegExp(r'^[\s|:-]+$');

/// Разбирает размеченный текст на блоки.
List<ProseBlock> parseProse(String md) {
  final blocks = <ProseBlock>[];
  final paragraph = StringBuffer();
  // Выноска и таблица копятся по строкам: в источнике они многострочны, а
  // блоком становятся целиком. Абзац копится тем же буфером, и оба закрытия
  // зовутся перед всяким другим блоком — иначе «> Примечание» приклеилось
  // бы к следующему подзаголовку.
  final quote = StringBuffer();
  final table = <List<String>>[];
  var heads = false;

  void closeParagraph() {
    final text = paragraph.toString().trim();
    paragraph.clear();
    if (text.isEmpty) return;
    blocks.add(ProseBlock(kind: ProseKind.paragraph, text: text));
  }

  void closeQuote() {
    final text = quote.toString().trim();
    quote.clear();
    if (text.isEmpty) return;
    blocks.add(ProseBlock(kind: ProseKind.quote, text: text));
  }

  void closeTable() {
    if (table.isEmpty) return;
    final rows = [for (final one in table) List<String>.unmodifiable(one)];
    final withHeads = heads;
    table.clear();
    heads = false;
    // Таблица из одной строки таблицей не является: это строка с чертами,
    // и сеткой она выглядела бы разметкой, показанной как содержимое.
    if (rows.length < 2 && !withHeads) {
      blocks.add(
        ProseBlock(kind: ProseKind.paragraph, text: rows.first.join(' — ')),
      );
      return;
    }
    blocks.add(
      ProseBlock(
        kind: ProseKind.table,
        text: '',
        rows: List<List<String>>.unmodifiable(rows),
        heads: withHeads,
      ),
    );
  }

  void closeAll() {
    closeParagraph();
    closeQuote();
    closeTable();
  }

  for (final raw in md.split('\n')) {
    final line = raw.trimRight();
    if (line.trim().isEmpty) {
      closeAll();
      continue;
    }

    final row = _row.firstMatch(line);
    if (row != null) {
      closeParagraph();
      closeQuote();
      // Замыкающая черта — ограничитель, а не пустая ячейка: без её
      // снятия у каждой строки появляется лишний пустой столбец, и шапка
      // перестаёт сходиться с телом по числу колонок.
      var inner = row.group(1)!.trimRight();
      if (inner.endsWith('|')) inner = inner.substring(0, inner.length - 1);
      final cells = [for (final cell in inner.split('|')) cell.trim()];
      // Разделитель шапки помечает предыдущую строку шапкой и сам в
      // таблицу не ложится.
      if (_rowBreak.hasMatch(inner) &&
          inner.contains('-') &&
          table.length == 1) {
        heads = true;
        continue;
      }
      table.add(cells);
      continue;
    }
    closeTable();

    final quoted = _quote.firstMatch(line);
    if (quoted != null) {
      closeParagraph();
      // Перенос внутри выноски склеивается так же, как внутри абзаца.
      if (quote.isNotEmpty) quote.write(' ');
      quote.write(quoted.group(1)!.trim());
      continue;
    }
    closeQuote();

    if (_rule.hasMatch(line)) {
      closeParagraph();
      blocks.add(const ProseBlock(kind: ProseKind.rule, text: ''));
      continue;
    }

    final heading = _heading.firstMatch(line);
    if (heading != null) {
      closeParagraph();
      blocks.add(
        ProseBlock(
          kind: ProseKind.heading,
          text: heading.group(2)!.trim(),
          level: heading.group(1)!.length,
        ),
      );
      continue;
    }

    final numbered = _numbered.firstMatch(line);
    if (numbered != null) {
      closeParagraph();
      blocks.add(
        ProseBlock(
          kind: ProseKind.numbered,
          text: numbered.group(3)!.trim(),
          level: _depth(numbered.group(1)!),
          marker: numbered.group(2)!,
        ),
      );
      continue;
    }

    final bullet = _bullet.firstMatch(line);
    if (bullet != null) {
      closeParagraph();
      blocks.add(
        ProseBlock(
          kind: ProseKind.bullet,
          text: bullet.group(2)!.trim(),
          level: _depth(bullet.group(1)!),
        ),
      );
      continue;
    }

    // Мягкий перенос строки внутри абзаца склеивается пробелом: в
    // источнике абзац бывает разложен по строкам ради ширины файла, а на
    // экране он обязан переноситься по месту, а не по чужой ширине.
    if (paragraph.isNotEmpty) paragraph.write(' ');
    paragraph.write(line.trim());
  }
  closeAll();
  return blocks;
}

/// Вложенность по отступу: два пробела — один уровень.
int _depth(String indent) => (indent.length ~/ 2).clamp(0, 3);

/// Разбирает внутреннюю разметку строки в куски показа.
///
/// Переносы расставляются здесь, а не в разборе блоков: мягкий перенос —
/// это свойство показа, и в тексте, уехавшем в поиск или в буфер обмена,
/// его быть не должно.
List<InlineSpan> proseSpans(
  String raw, {
  required TextStyle? base,
  bool hyphens = true,
}) {
  final spans = <InlineSpan>[];
  final plain = StringBuffer();

  void flush() {
    if (plain.isEmpty) return;
    spans.add(TextSpan(text: _shown(plain.toString(), hyphens), style: base));
    plain.clear();
  }

  var i = 0;
  while (i < raw.length) {
    final closed = _marked(raw, i);
    if (closed != null) {
      flush();
      spans.add(
        TextSpan(
          text: _shown(closed.text, hyphens && closed.style != _Mark.code),
          style: _styled(base, closed.style),
        ),
      );
      i = closed.end;
      continue;
    }
    plain.write(raw[i]);
    i++;
  }
  flush();
  return spans;
}

String _shown(String text, bool hyphens) => hyphens ? hyphenate(text) : text;

enum _Mark { strong, emphasis, code }

class _Marked {
  const _Marked(this.text, this.end, this.style);
  final String text;
  final int end;
  final _Mark style;
}

/// Находит выделение, начинающееся в [at].
///
/// Незакрытое выделение не считается выделением: звёздочка посреди фразы
/// встречается как знак сноски, и съесть из-за неё остаток абзаца хуже,
/// чем показать звёздочку.
_Marked? _marked(String raw, int at) {
  for (final pair in const [
    ('**', _Mark.strong),
    ('__', _Mark.strong),
    ('*', _Mark.emphasis),
    ('`', _Mark.code),
  ]) {
    final (open, style) = pair;
    if (!raw.startsWith(open, at)) continue;
    final from = at + open.length;
    final close = raw.indexOf(open, from);
    if (close <= from) continue;
    return _Marked(raw.substring(from, close), close + open.length, style);
  }
  return null;
}

TextStyle? _styled(TextStyle? base, _Mark style) => switch (style) {
  _Mark.strong => (base ?? const TextStyle()).copyWith(
    fontWeight: FontWeight.w600,
  ),
  _Mark.emphasis => (base ?? const TextStyle()).copyWith(
    fontStyle: FontStyle.italic,
  ),
  _Mark.code => (base ?? const TextStyle()).copyWith(
    fontFamily: 'monospace',
    fontFamilyFallback: const ['RobotoMono', 'Courier'],
  ),
};

/// Размеченный текст на экране.
///
/// Переносы по правилам русского языка включены по умолчанию: столбец на
/// телефоне узкий, и без них «дифференциальный» либо уезжает за край,
/// либо оставляет полстроки пустоты.
class Prose extends StatelessWidget {
  const Prose(
    this.text, {
    super.key,
    this.style,
    this.align = TextAlign.left,
    this.hyphens = true,
  });

  final String text;
  final TextStyle? style;
  final TextAlign align;
  final bool hyphens;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final base = style ?? theme.textTheme.bodyLarge;
    final blocks = parseProse(text);
    if (blocks.isEmpty) return const SizedBox.shrink();

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var i = 0; i < blocks.length; i++)
          Padding(
            padding: EdgeInsets.only(top: i == 0 ? 0 : _gap(blocks[i])),
            child: _block(context, blocks[i], base),
          ),
      ],
    );
  }

  double _gap(ProseBlock block) => switch (block.kind) {
    // Подзаголовок отделяется от предыдущего сильнее, чем абзац от
    // абзаца: он начинает новое, а не продолжает прежнее.
    ProseKind.heading => 16,
    ProseKind.rule => 16,
    ProseKind.table => 14,
    ProseKind.quote => 12,
    ProseKind.paragraph => 10,
    _ => 6,
  };

  Widget _block(BuildContext context, ProseBlock block, TextStyle? base) {
    final theme = Theme.of(context);
    switch (block.kind) {
      case ProseKind.heading:
        final style = block.level <= 2
            ? theme.textTheme.titleSmall
            : theme.textTheme.labelLarge;
        return _line(block.text, style, hyphens: false);

      case ProseKind.rule:
        // Волосяная линия, а не тень и не полоса: она разделяет плоскости,
        // лежащие на одной странице.
        return Divider(
          height: 1,
          thickness: 1,
          color: theme.colorScheme.outlineVariant,
        );

      case ProseKind.quote:
        // Выноска отделена чертой слева, а не заливкой: заливка на светлой
        // и тёмной теме ведёт себя по-разному, а черта — одинаково.
        return Container(
          padding: const EdgeInsets.only(left: 12),
          decoration: BoxDecoration(
            border: Border(
              left: BorderSide(
                color: theme.colorScheme.outlineVariant,
                width: 2,
              ),
            ),
          ),
          child: _line(
            block.text,
            base?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        );

      case ProseKind.table:
        return ProseTable(rows: block.rows, heads: block.heads, base: base);

      case ProseKind.paragraph:
        return _line(block.text, base);

      case ProseKind.bullet:
      case ProseKind.numbered:
        final marker = block.kind == ProseKind.numbered
            ? '${block.marker}.'
            : '•';
        return Padding(
          padding: EdgeInsets.only(left: 12.0 * block.level),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // Маркер в колонке постоянной ширины: без неё пункты «9.» и
              // «10.» начинаются с разных мест, и список выглядит кривым.
              SizedBox(
                width: block.kind == ProseKind.numbered ? 28 : 18,
                child: Text(
                  marker,
                  style: base?.copyWith(
                    color: theme.colorScheme.onSurfaceVariant,
                    fontFeatures: const [FontFeature.tabularFigures()],
                  ),
                ),
              ),
              Expanded(child: _line(block.text, base)),
            ],
          ),
        );
    }
  }

  Widget _line(String text, TextStyle? style, {bool? hyphens}) => Text.rich(
    TextSpan(
      children: proseSpans(text, base: style, hyphens: hyphens ?? this.hyphens),
    ),
    textAlign: align,
  );
}

/// Таблица разметки, показанная по ширине телефона.
///
/// Вид выбирается по самой таблице, а не задаётся сверху. Короткие ячейки
/// («F10.2 | Синдром зависимости») читаются сеткой: колонки стоят друг под
/// другом, и глаз сравнивает строки. Длинные ячейки в сетке дают три
/// колонки по паре слов в строке — столбик слов вместо сравнения, и каждая
/// строка высотой в экран. Такие показываются построчно: шапка становится
/// подписью над значением.
///
/// Порог замерен по материалу справочника, а не выбран на глаз: у таблиц с
/// кодами ячейки короче сорока знаков, у дифференциального диагноза
/// доходят до двухсот двадцати.
class ProseTable extends StatelessWidget {
  const ProseTable({
    super.key,
    required this.rows,
    required this.heads,
    this.base,
  });

  final List<List<String>> rows;
  final bool heads;
  final TextStyle? base;

  /// Длина ячейки, после которой сетка перестаёт читаться.
  static const int gridCell = 40;

  /// Помещается ли таблица сеткой.
  static bool asGrid(List<List<String>> rows) {
    if (rows.isEmpty) return false;
    // Больше трёх колонок сеткой на телефоне не помещается ни при какой
    // длине ячейки: на колонку остаётся меньше сантиметра.
    if (rows.any((row) => row.length > 3)) return false;
    return rows.every((row) => row.every((cell) => cell.length <= gridCell));
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (rows.isEmpty) return const SizedBox.shrink();
    final hairline = BorderSide(color: theme.colorScheme.outlineVariant);
    final headStyle = base?.copyWith(fontWeight: FontWeight.w600);

    if (asGrid(rows)) {
      return Table(
        border: TableBorder(
          horizontalInside: hairline,
          top: hairline,
          bottom: hairline,
        ),
        // Первая колонка таблиц справочника — это код или признак, и она
        // узкая; ширина по содержимому не даёт ей разъехаться на пол-экрана.
        defaultColumnWidth: const IntrinsicColumnWidth(),
        columnWidths: const {1: FlexColumnWidth()},
        defaultVerticalAlignment: TableCellVerticalAlignment.top,
        children: [
          for (var r = 0; r < rows.length; r++)
            TableRow(
              children: [
                for (final cell in rows[r])
                  Padding(
                    padding: const EdgeInsets.fromLTRB(0, 7, 12, 7),
                    child: Prose(
                      cell,
                      style: heads && r == 0 ? headStyle : base,
                      hyphens: false,
                    ),
                  ),
              ],
            ),
        ],
      );
    }

    final header = heads ? rows.first : const <String>[];
    final body = heads ? rows.skip(1).toList() : rows;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var r = 0; r < body.length; r++)
          Container(
            margin: EdgeInsets.only(top: r == 0 ? 0 : 10),
            padding: const EdgeInsets.only(left: 12),
            decoration: BoxDecoration(border: Border(left: hairline)),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                for (var c = 0; c < body[r].length; c++) ...[
                  if (c < header.length && header[c].isNotEmpty)
                    Padding(
                      padding: EdgeInsets.only(top: c == 0 ? 0 : 8, bottom: 2),
                      child: Text(
                        header[c],
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.primary,
                        ),
                      ),
                    ),
                  Prose(body[r][c], style: base),
                ],
              ],
            ),
          ),
      ],
    );
  }
}
