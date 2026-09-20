import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'palette.dart';
import 'tokens.dart';
import 'typography.dart';

/// Сборка ThemeData из палитры.
///
/// Плоскости различаются цветом и волосяной границей — теней в приложении
/// нет: они дают «пластиковый» вид и плохо читаются на тёплом фоне.
///
/// Палитра лежит в [AppPalette], а строение темы — здесь: какой цвет куда
/// назначен, какие радиусы, какие кегли. Разделение сохранено от донора,
/// где палитру подменял бренд организации: перекрасить приложение можно
/// было, переустроить — нет. Организаций здесь нет, но граница полезна и
/// без них — она не даёт назначению цвета расползтись по экранам.
abstract final class AppTheme {
  static ThemeData light() => _build(AppPalette.light);
  static ThemeData dark() => _build(AppPalette.dark);

  static ThemeData _build(AppPalette p) {
    final scheme = ColorScheme(
      brightness: p.brightness,
      primary: p.accent,
      onPrimary: p.accentInk,
      primaryContainer: p.accentSoft,
      onPrimaryContainer: p.accent,
      secondary: p.success,
      onSecondary: p.isDark ? const Color(0xFF16200D) : Colors.white,
      secondaryContainer: p.successSoft,
      onSecondaryContainer: p.success,
      tertiary: p.warning,
      onTertiary: Colors.white,
      tertiaryContainer: p.warningSoft,
      onTertiaryContainer: p.warning,
      error: p.danger,
      onError: Colors.white,
      errorContainer: p.dangerSoft,
      onErrorContainer: p.danger,
      surface: p.surface,
      onSurface: p.ink,
      surfaceContainerLowest: p.surface,
      surfaceContainerLow: p.surface,
      surfaceContainer: p.surfaceMuted,
      surfaceContainerHigh: p.surfaceMuted,
      surfaceContainerHighest: p.surfaceMuted,
      onSurfaceVariant: p.inkMuted,
      outline: p.hairline,
      outlineVariant: p.hairline,
      shadow: Colors.black,
      scrim: Colors.black,
      inverseSurface: p.ink,
      onInverseSurface: p.canvas,
      inversePrimary: p.accentSoft,
    );

    final text = AppType.theme(p.ink, p.inkMuted);

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      scaffoldBackgroundColor: p.canvas,
      canvasColor: p.canvas,
      fontFamily: AppType.family,
      textTheme: text,
      splashFactory: InkSparkle.splashFactory,
      extensions: [p],
      appBarTheme: AppBarTheme(
        backgroundColor: p.canvas,
        surfaceTintColor: Colors.transparent,
        foregroundColor: p.ink,
        elevation: 0,
        scrolledUnderElevation: 0,
        centerTitle: false,
        titleTextStyle: AppType.titleM.copyWith(color: p.ink),
        systemOverlayStyle: p.isDark
            ? SystemUiOverlayStyle.light
            : SystemUiOverlayStyle.dark,
      ),
      dividerTheme: DividerThemeData(color: p.hairline, thickness: 1, space: 1),
      cardTheme: CardThemeData(
        color: p.surface,
        surfaceTintColor: Colors.transparent,
        elevation: 0,
        margin: EdgeInsets.zero,
        shape: RoundedRectangleBorder(
          borderRadius: Radii.surfaceAll,
          side: BorderSide(color: p.hairline),
        ),
      ),
      listTileTheme: ListTileThemeData(
        contentPadding: const EdgeInsets.symmetric(
          horizontal: Gap.lg,
          vertical: Gap.sm,
        ),
        titleTextStyle: AppType.titleS.copyWith(color: p.ink),
        subtitleTextStyle: AppType.caption.copyWith(color: p.inkMuted),
        iconColor: p.inkMuted,
        shape: const RoundedRectangleBorder(borderRadius: Radii.surfaceAll),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          backgroundColor: p.accent,
          foregroundColor: p.accentInk,
          disabledBackgroundColor: p.surfaceMuted,
          disabledForegroundColor: p.inkFaint,
          minimumSize: const Size(0, 52),
          padding: const EdgeInsets.symmetric(horizontal: Gap.xl),
          textStyle: AppType.bodyStrong,
          shape: const RoundedRectangleBorder(borderRadius: Radii.controlAll),
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          foregroundColor: p.ink,
          side: BorderSide(color: p.hairline),
          minimumSize: const Size(0, 52),
          padding: const EdgeInsets.symmetric(horizontal: Gap.xl),
          textStyle: AppType.bodyStrong,
          shape: const RoundedRectangleBorder(borderRadius: Radii.controlAll),
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          foregroundColor: p.inkMuted,
          minimumSize: const Size(0, 48),
          textStyle: AppType.bodyStrong,
          shape: const RoundedRectangleBorder(borderRadius: Radii.controlAll),
        ),
      ),
      chipTheme: ChipThemeData(
        backgroundColor: p.surface,
        side: BorderSide(color: p.hairline),
        labelStyle: AppType.label.copyWith(color: p.ink),
        padding: const EdgeInsets.symmetric(
          horizontal: Gap.md,
          vertical: Gap.sm,
        ),
        shape: const RoundedRectangleBorder(borderRadius: Radii.controlAll),
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: p.surface,
        surfaceTintColor: Colors.transparent,
        indicatorColor: p.accentSoft,
        elevation: 0,
        height: 68,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        indicatorShape: const RoundedRectangleBorder(
          borderRadius: Radii.controlAll,
        ),
        labelTextStyle: WidgetStateProperty.resolveWith(
          (states) => states.contains(WidgetState.selected)
              ? AppType.caption.copyWith(
                  color: p.accent,
                  fontWeight: FontWeight.w600,
                )
              : AppType.caption.copyWith(color: p.inkFaint),
        ),
        iconTheme: WidgetStateProperty.resolveWith(
          (states) => IconThemeData(
            size: 22,
            color: states.contains(WidgetState.selected)
                ? p.accent
                : p.inkFaint,
          ),
        ),
      ),
      searchBarTheme: SearchBarThemeData(
        backgroundColor: WidgetStatePropertyAll(p.surface),
        surfaceTintColor: const WidgetStatePropertyAll(Colors.transparent),
        overlayColor: const WidgetStatePropertyAll(Colors.transparent),
        elevation: const WidgetStatePropertyAll(0),
        side: WidgetStatePropertyAll(BorderSide(color: p.hairline)),
        shape: const WidgetStatePropertyAll(
          RoundedRectangleBorder(borderRadius: Radii.controlAll),
        ),
        padding: const WidgetStatePropertyAll(
          EdgeInsets.symmetric(horizontal: Gap.lg),
        ),
        hintStyle: WidgetStatePropertyAll(
          AppType.body.copyWith(color: p.inkFaint),
        ),
        textStyle: WidgetStatePropertyAll(AppType.body.copyWith(color: p.ink)),
      ),
      snackBarTheme: SnackBarThemeData(
        backgroundColor: p.ink,
        contentTextStyle: AppType.body.copyWith(color: p.canvas),
        behavior: SnackBarBehavior.floating,
        shape: const RoundedRectangleBorder(borderRadius: Radii.controlAll),
      ),
      progressIndicatorTheme: ProgressIndicatorThemeData(
        color: p.accent,
        linearTrackColor: p.surfaceMuted,
        circularTrackColor: p.surfaceMuted,
      ),
      bottomSheetTheme: BottomSheetThemeData(
        backgroundColor: p.canvas,
        surfaceTintColor: Colors.transparent,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radii.surface),
        ),
      ),
    );
  }
}
