import 'package:flutter/material.dart';

/// Цветовая система приложения.
///
/// Подход — «энергия поверх бумаги»: тёплый небелый фон и чернильный текст
/// остаются основой, потому что положения источника читают подолгу.
/// Живость даёт не заливка экрана, а насыщенный акцент, градиент [energy]
/// на индикаторах прогресса и цветные подложки под метриками.
///
/// Тональность — землистая четвёрка: песочная бумага, чернила глубокой
/// бирюзы, терракотовая энергия и оливковый успех.
///
/// Цвета до последней цифры взяты у приложения-донора, и это условие
/// работы, а не удобство: два приложения одного владельца, разошедшиеся
/// в оттенке фона, выглядят не как семейство, а как поделка. Оттенки
/// здесь не подбирались заново — они перенесены.
///
/// Плоскости по-прежнему разделяются волосяными границами, а не тенями.
/// Красный зарезервирован строго за неверным ответом, поэтому в нейтральном
/// интерфейсе его нет; тёплый [streak] — про серию дней, а не про ошибку.
@immutable
class AppPalette extends ThemeExtension<AppPalette> {
  const AppPalette({
    required this.canvas,
    required this.surface,
    required this.surfaceMuted,
    required this.hairline,
    required this.ink,
    required this.inkMuted,
    required this.inkFaint,
    required this.accent,
    required this.accentSoft,
    required this.accentInk,
    required this.spark,
    required this.sparkSoft,
    required this.energyMid,
    required this.heroCanvas,
    required this.heroCanvasAlt,
    required this.heroGlass,
    required this.heroHairline,
    required this.heroInk,
    required this.heroInkMuted,
    required this.streak,
    required this.streakSoft,
    required this.success,
    required this.successSoft,
    required this.warning,
    required this.warningSoft,
    required this.danger,
    required this.dangerSoft,
    required this.brightness,
  });

  final Color canvas; // фон экрана
  final Color surface; // карточки и поля
  final Color surfaceMuted; // вложенные плоскости
  final Color hairline; // границы 1 px
  final Color ink; // основной текст
  final Color inkMuted; // вторичный текст
  final Color inkFaint; // подписи, плейсхолдеры
  final Color accent; // основной акцент
  final Color accentSoft; // фон-подложка акцента
  final Color accentInk; // текст на accent

  /// Вторая точка градиента [energy]. Декоративный цвет: им заливают дуги,
  /// полосы и бейджи, но не мелкий текст — контраст к бумаге у него 3:1,
  /// достаточный для графики и недостаточный для подписей.
  final Color spark;
  final Color sparkSoft;

  /// Мост градиента [energy] — аметист между бирюзой и розой.
  ///
  /// Путь по цветовому кругу монотонный (бирюза → фиолет → маджента),
  /// каждое плечо короткое, поэтому sRGB-интерполяция нигде не проходит
  /// через серую середину: между стопами лежат насыщенные синий и пурпур.
  final Color energyMid;

  /// Тёмная hero-зона экрана «Прогресс» — «ночная глубина» над песком.
  ///
  /// Игровая шапка живёт в собственной мини-теме: глубокая вода бренда,
  /// уходящая в аметистовую ночь [heroCanvasAlt], стеклянные плитки
  /// [heroGlass] с волосяными границами [heroHairline] и песчаный текст
  /// [heroInk]/[heroInkMuted]. Токены общие для обеих тем, чтобы герой
  /// выглядел одинаково ночным и там и там.
  final Color heroCanvas;
  final Color heroCanvasAlt;
  final Color heroGlass;
  final Color heroHairline;
  final Color heroInk;
  final Color heroInkMuted;

  /// Градиент hero-зоны: глубина воды, уходящая в аметистовую ночь.
  LinearGradient get heroGradient => LinearGradient(
    colors: [heroCanvas, heroCanvasAlt],
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  );

  /// Серия дней подряд. Тёплый, но не красный: красный занят ошибкой.
  final Color streak;
  final Color streakSoft;

  final Color success;
  final Color successSoft;
  final Color warning;
  final Color warningSoft;
  final Color danger;
  final Color dangerSoft;
  final Brightness brightness;

  bool get isDark => brightness == Brightness.dark;

  /// Фирменный градиент прогресса: глубокая бирюза → аметист → роза.
  ///
  /// «Полярное сияние» поверх бумаги: заполненность разгорается из
  /// глубины акцента к неону. Один и тот же переход на кольце точности,
  /// полосе опыта и полосах освоенности блоков — так «заполненность»
  /// читается как одна величина. Три стопа, а не два: см. [energyMid].
  List<Color> get energy => [accent, energyMid, spark];

  /// Градиент как заливка: угол один на всё приложение, чтобы блоки
  /// на экране выглядели освещёнными с одной стороны.
  LinearGradient get energyGradient => LinearGradient(
    colors: energy,
    begin: Alignment.topLeft,
    end: Alignment.bottomRight,
  );

  /// Светлая тема — «песок и глубокая вода».
  ///
  /// Четвёрка распределена по правилу 60-30-10:
  /// песок `#E6D7C3` — доминанта (бумага фона и вложенные плоскости),
  /// глубокая бирюза `#0D3B4E` — чернила текста и интерактив,
  /// терракота `#C85A3E` — тёплый акцент (серия дней, печать знака),
  /// олива `#5A6B44` — успех. Где роль требует другой светлоты
  /// (текст на подложке, кнопка под белой подписью), берётся тональный
  /// шаг того же тона, а не чужой цвет.
  ///
  /// Градиент [energy] — намеренно вне землистой четвёрки: земляные
  /// пары в интерполяции дают хаки, а не энергию. Сияние держат
  /// ювелирные аметист и роза, стартующие из бирюзы акцента.
  static const light = AppPalette(
    canvas: Color(0xFFF4EEE2),
    surface: Color(0xFFFCF9F1),
    surfaceMuted: Color(0xFFE6D7C3),
    hairline: Color(0xFFD8CBB2),
    ink: Color(0xFF0D3B4E),
    inkMuted: Color(0xFF43606E),
    inkFaint: Color(0xFF4E6875),
    accent: Color(0xFF155D7A),
    accentSoft: Color(0xFFDCE9EC),
    accentInk: Color(0xFFFFFFFF),
    spark: Color(0xFFD6366F),
    sparkSoft: Color(0xFFF9E1EB),
    energyMid: Color(0xFF7C4DC4),
    heroCanvas: Color(0xFF0A2836),
    heroCanvasAlt: Color(0xFF251C4A),
    heroGlass: Color(0x1AFFFFFF),
    heroHairline: Color(0x2EFFFFFF),
    heroInk: Color(0xFFF6F3EA),
    heroInkMuted: Color(0xFFBAC6CD),
    streak: Color(0xFFA8481F),
    streakSoft: Color(0xFFF8E4D6),
    success: Color(0xFF5A6B44),
    successSoft: Color(0xFFE9EBD8),
    warning: Color(0xFF9A6413),
    warningSoft: Color(0xFFF5E9CF),
    danger: Color(0xFFA32C25),
    dangerSoft: Color(0xFFF8E5E2),
    brightness: Brightness.light,
  );

  /// Тёмная тема бренда по умолчанию — те же четыре тона при
  /// инвертированных ролях: холст
  /// уходит в глубину бирюзы, «чернилами» становится песок, а терракота
  /// и олива осветляются до читаемых на тёмном оттенков.
  static const dark = AppPalette(
    canvas: Color(0xFF0B1418),
    surface: Color(0xFF111C22),
    surfaceMuted: Color(0xFF18262E),
    hairline: Color(0xFF263740),
    ink: Color(0xFFEAE3D3),
    inkMuted: Color(0xFFA7B3B3),
    inkFaint: Color(0xFF93A2A6),
    accent: Color(0xFF7FB6C9),
    accentSoft: Color(0xFF123240),
    accentInk: Color(0xFF07242F),
    spark: Color(0xFFF27FAE),
    sparkSoft: Color(0xFF38182A),
    energyMid: Color(0xFFA78BFA),
    heroCanvas: Color(0xFF0C2431),
    heroCanvasAlt: Color(0xFF241C46),
    heroGlass: Color(0x1AFFFFFF),
    heroHairline: Color(0x2EFFFFFF),
    heroInk: Color(0xFFF0EBDE),
    heroInkMuted: Color(0xFFB4C1C9),
    streak: Color(0xFFE89E74),
    streakSoft: Color(0xFF3A2617),
    success: Color(0xFF9CB478),
    successSoft: Color(0xFF202B16),
    warning: Color(0xFFD9A75B),
    warningSoft: Color(0xFF2E2412),
    danger: Color(0xFFE97A72),
    dangerSoft: Color(0xFF341B18),
    brightness: Brightness.dark,
  );

  @override
  AppPalette copyWith({
    Color? canvas,
    Color? surface,
    Color? surfaceMuted,
    Color? hairline,
    Color? ink,
    Color? inkMuted,
    Color? inkFaint,
    Color? accent,
    Color? accentSoft,
    Color? accentInk,
    Color? spark,
    Color? sparkSoft,
    Color? energyMid,
    Color? heroCanvas,
    Color? heroCanvasAlt,
    Color? heroGlass,
    Color? heroHairline,
    Color? heroInk,
    Color? heroInkMuted,
    Color? streak,
    Color? streakSoft,
    Color? success,
    Color? successSoft,
    Color? warning,
    Color? warningSoft,
    Color? danger,
    Color? dangerSoft,
    Brightness? brightness,
  }) => AppPalette(
    canvas: canvas ?? this.canvas,
    surface: surface ?? this.surface,
    surfaceMuted: surfaceMuted ?? this.surfaceMuted,
    hairline: hairline ?? this.hairline,
    ink: ink ?? this.ink,
    inkMuted: inkMuted ?? this.inkMuted,
    inkFaint: inkFaint ?? this.inkFaint,
    accent: accent ?? this.accent,
    accentSoft: accentSoft ?? this.accentSoft,
    accentInk: accentInk ?? this.accentInk,
    spark: spark ?? this.spark,
    sparkSoft: sparkSoft ?? this.sparkSoft,
    energyMid: energyMid ?? this.energyMid,
    heroCanvas: heroCanvas ?? this.heroCanvas,
    heroCanvasAlt: heroCanvasAlt ?? this.heroCanvasAlt,
    heroGlass: heroGlass ?? this.heroGlass,
    heroHairline: heroHairline ?? this.heroHairline,
    heroInk: heroInk ?? this.heroInk,
    heroInkMuted: heroInkMuted ?? this.heroInkMuted,
    streak: streak ?? this.streak,
    streakSoft: streakSoft ?? this.streakSoft,
    success: success ?? this.success,
    successSoft: successSoft ?? this.successSoft,
    warning: warning ?? this.warning,
    warningSoft: warningSoft ?? this.warningSoft,
    danger: danger ?? this.danger,
    dangerSoft: dangerSoft ?? this.dangerSoft,
    brightness: brightness ?? this.brightness,
  );

  @override
  AppPalette lerp(AppPalette? other, double t) {
    if (other == null) return this;
    Color c(Color a, Color b) => Color.lerp(a, b, t)!;
    return AppPalette(
      canvas: c(canvas, other.canvas),
      surface: c(surface, other.surface),
      surfaceMuted: c(surfaceMuted, other.surfaceMuted),
      hairline: c(hairline, other.hairline),
      ink: c(ink, other.ink),
      inkMuted: c(inkMuted, other.inkMuted),
      inkFaint: c(inkFaint, other.inkFaint),
      accent: c(accent, other.accent),
      accentSoft: c(accentSoft, other.accentSoft),
      accentInk: c(accentInk, other.accentInk),
      spark: c(spark, other.spark),
      sparkSoft: c(sparkSoft, other.sparkSoft),
      energyMid: c(energyMid, other.energyMid),
      heroCanvas: c(heroCanvas, other.heroCanvas),
      heroCanvasAlt: c(heroCanvasAlt, other.heroCanvasAlt),
      heroGlass: c(heroGlass, other.heroGlass),
      heroHairline: c(heroHairline, other.heroHairline),
      heroInk: c(heroInk, other.heroInk),
      heroInkMuted: c(heroInkMuted, other.heroInkMuted),
      streak: c(streak, other.streak),
      streakSoft: c(streakSoft, other.streakSoft),
      success: c(success, other.success),
      successSoft: c(successSoft, other.successSoft),
      warning: c(warning, other.warning),
      warningSoft: c(warningSoft, other.warningSoft),
      danger: c(danger, other.danger),
      dangerSoft: c(dangerSoft, other.dangerSoft),
      brightness: t < 0.5 ? brightness : other.brightness,
    );
  }
}

/// Быстрый доступ к палитре: `context.palette.accent`.
///
/// Если тема собрана без расширения (например, виджет показан под голым
/// MaterialApp), берётся палитра по яркости — отсутствие токенов не должно
/// ронять экран.
///
/// У донора запасной путь ходил за палитрой к бренду сборки: там одна
/// установка обслуживает несколько организаций, и вернуть свой цвет под
/// чужим брендом значило бы покрасить чужое приложение нашим. Здесь
/// организаций нет вовсе, палитра одна, и лишний уровень косвенности был
/// бы вторым местом для тех же цветов.
extension PaletteAccess on BuildContext {
  AppPalette get palette {
    final theme = Theme.of(this);
    return theme.extension<AppPalette>() ??
        (theme.brightness == Brightness.dark
            ? AppPalette.dark
            : AppPalette.light);
  }
}
