import 'package:flutter/material.dart';

/// Иконка или маркер, выровненный по первой строке соседнего текста.
///
/// В строке «иконка + абзац» иконку нельзя центрировать по всему тексту:
/// на многострочном абзаце она уедет в середину. Прижимать к верху тоже
/// нельзя — она встанет выше строчных букв. Здесь маркер центрируется в
/// коробке высотой в одну строку, поэтому его оптическая ось совпадает с
/// осью первой строки при любой длине текста.
class LeadingGlyph extends StatelessWidget {
  const LeadingGlyph({
    super.key,
    required this.child,
    required this.lineStyle,
    this.width,
  });

  /// Сам маркер: иконка, буква пункта, точка.
  final Widget child;

  /// Стиль текста, рядом с которым стоит маркер: из него берётся высота
  /// строки, по которой идёт выравнивание.
  final TextStyle lineStyle;

  /// Ширина колонки маркера. Задаётся, когда нужна общая линия отступа
  /// для нескольких строк списка.
  final double? width;

  @override
  Widget build(BuildContext context) {
    final scale = MediaQuery.textScalerOf(context);
    final fontSize = scale.scale(lineStyle.fontSize ?? 14);
    final lineHeight = fontSize * (lineStyle.height ?? 1.4);

    return SizedBox(
      width: width,
      height: lineHeight,
      child: Align(
        alignment: width == null ? Alignment.center : Alignment.centerLeft,
        child: child,
      ),
    );
  }
}
