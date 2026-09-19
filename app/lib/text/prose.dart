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
/// и нумерованные списки, подзаголовки, полужирное, курсив и моноширинное.
/// Полный Markdown — это зависимость, а зависимость здесь называется
/// вслух вместе с тем, чего без неё нельзя; таблицы и ссылки в критериях
/// не встречаются, и тянуть ради них пакет не за что.
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
}

/// Блок разобранного текста.
class ProseBlock {
  const ProseBlock({
    required this.kind,
    required this.text,
    this.level = 0,
    this.marker = '',
  });

  final ProseKind kind;

  /// Текст блока с внутренней разметкой — она разбирается при показе.
  final String text;

  /// Вложенность списка или глубина подзаголовка.
  final int level;

  /// Номер пункта, как он записан в источнике.
  final String marker;
}

final _bullet = RegExp(r'^(\s*)[-*•]\s+(.*)$');
final _numbered = RegExp(r'^(\s*)(\d+)[.)]\s+(.*)$');
final _heading = RegExp(r'^(#{1,4})\s+(.*)$');

/// Разбирает размеченный текст на блоки.
List<ProseBlock> parseProse(String md) {
  final blocks = <ProseBlock>[];
  final paragraph = StringBuffer();

  void closeParagraph() {
    final text = paragraph.toString().trim();
    paragraph.clear();
    if (text.isEmpty) return;
    blocks.add(ProseBlock(kind: ProseKind.paragraph, text: text));
  }

  for (final raw in md.split('\n')) {
    final line = raw.trimRight();
    if (line.trim().isEmpty) {
      closeParagraph();
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
  closeParagraph();
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
