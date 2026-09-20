import 'package:flutter/material.dart';

/// Типографика на Golos Text — вариативном шрифте с кириллицей.
///
/// Начертание задаётся осью `wght` через [FontVariation]: у вариативного
/// шрифта одного `fontWeight` недостаточно, движок обязан получить ось.
abstract final class AppType {
  static const family = 'GolosText';

  static List<FontVariation> _w(double weight) => [
    FontVariation('wght', weight),
  ];

  static TextStyle _s({
    required double size,
    required double height,
    required double weight,
    double tracking = 0,
  }) => TextStyle(
    fontFamily: family,
    fontSize: size,
    height: height / size,
    letterSpacing: tracking,
    fontWeight: FontWeight.values[(weight ~/ 100) - 1],
    fontVariations: _w(weight),
  );

  /// Заголовок экрана.
  static final display = _s(size: 30, height: 36, weight: 700, tracking: -0.6);
  static final titleL = _s(size: 22, height: 28, weight: 600, tracking: -0.3);
  static final titleM = _s(size: 17, height: 23, weight: 600, tracking: -0.1);
  static final titleS = _s(size: 15, height: 20, weight: 600);

  /// Основной текст интерфейса.
  static final body = _s(size: 15, height: 21, weight: 400);

  /// Длинные тексты для чтения — условие задачи, критерии.
  static final reading = _s(size: 16, height: 25, weight: 400);
  static final bodyStrong = _s(size: 15, height: 21, weight: 500);
  static final label = _s(size: 13, height: 17, weight: 500);
  static final caption = _s(size: 12, height: 16, weight: 400);

  /// Мелкий прописной заголовок секции.
  static final overline = _s(size: 11, height: 14, weight: 600, tracking: 0.9);

  /// Крупные числа в статистике: моноширинные цифры, чтобы не «прыгали».
  static final numeral = _s(
    size: 30,
    height: 34,
    weight: 600,
    tracking: -0.5,
  ).copyWith(fontFeatures: const [FontFeature.tabularFigures()]);

  /// Главное число экрана «Прогресс»: уровень, серия дней.
  ///
  /// Отдельный стиль, а не `numeral.copyWith(fontSize:)`: на этом размере
  /// нужен и более плотный интерлиньяж, и более тугой трекинг.
  static final numeralHero = _s(
    size: 46,
    height: 48,
    weight: 700,
    tracking: -1.4,
  ).copyWith(fontFeatures: const [FontFeature.tabularFigures()]);

  /// Номер задачи — «000123».
  ///
  /// Заголовками задачи не различались: «Пациент Д., 22 года» встречается в
  /// наборе не раз. Номер уникален, и читают его как код, а не как фразу:
  /// моноширинные цифры выстраивают ведущие нули в ровную колонку, а трекинг
  /// не даёт им слипнуться в одно длинное число.
  static final caseNumber = _s(
    size: 17,
    height: 23,
    weight: 600,
    tracking: 0.6,
  ).copyWith(fontFeatures: const [FontFeature.tabularFigures()]);

  static TextTheme theme(Color ink, Color inkMuted) => TextTheme(
    displaySmall: display.copyWith(color: ink),
    headlineMedium: display.copyWith(color: ink),
    headlineSmall: titleL.copyWith(color: ink),
    titleLarge: titleL.copyWith(color: ink),
    titleMedium: titleM.copyWith(color: ink),
    titleSmall: titleS.copyWith(color: ink),
    bodyLarge: reading.copyWith(color: ink),
    bodyMedium: body.copyWith(color: ink),
    bodySmall: caption.copyWith(color: inkMuted),
    labelLarge: bodyStrong.copyWith(color: ink),
    labelMedium: label.copyWith(color: inkMuted),
    labelSmall: overline.copyWith(color: inkMuted),
  );
}
