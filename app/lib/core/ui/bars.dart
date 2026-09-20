import 'package:flutter/material.dart';

import '../design/palette.dart';
import '../design/tokens.dart';
import '../design/typography.dart';
import 'motion.dart';

/// Полоса заполнения со скруглёнными краями.
///
/// Базовый примитив для опыта и освоенности блоков: разные шкалы выглядят
/// одинаково, поэтому «сколько пройдено» читается везде одним движением.
class ProgressBar extends StatelessWidget {
  const ProgressBar({
    super.key,
    required this.value,
    this.gradient,
    this.color,
    this.track,
    this.height = 10,
    this.shimmer = false,
    this.duration = Motion.slow,
  });

  /// Доля заполнения, 0..1.
  final double value;

  /// Заливка градиентом. Если не задана — сплошной [color], иначе акцент.
  final Gradient? gradient;
  final Color? color;

  /// Цвет незаполненной части. По умолчанию — [AppPalette.surfaceMuted],
  /// но на цветной подложке его не видно, и тогда трек задают явно:
  /// пустая шкала обязана читаться как шкала, иначе её просто нет.
  final Color? track;

  final double height;

  /// Блик по заполненной части. Включают только там, где полоса живая:
  /// на пустой или завершённой шкале он выглядит шумом.
  final bool shimmer;
  final Duration duration;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final target = value.clamp(0.0, 1.0);

    return ClipRRect(
      borderRadius: Radii.pillAll,
      child: SizedBox(
        height: height,
        child: Stack(
          children: [
            Positioned.fill(child: ColoredBox(color: track ?? p.surfaceMuted)),
            // Positioned.fill даёт FractionallySizedBox тугие ограничения,
            // и тот растягивает заливку на всю высоту, отдавая ей долю
            // ширины. Без него бокс заливки остаётся без собственного
            // размера и схлопывается в ноль: трек виден, заполнения нет.
            Positioned.fill(
              child: TweenAnimationBuilder<double>(
                tween: Tween(begin: 0, end: target),
                duration: duration,
                curve: Motion.enter,
                builder: (context, v, _) => FractionallySizedBox(
                  alignment: Alignment.centerLeft,
                  widthFactor: v,
                  child: Shimmer(
                    // Блик имеет смысл только когда есть что подсвечивать.
                    enabled: shimmer && v > 0.02,
                    child: DecoratedBox(
                      decoration: BoxDecoration(
                        gradient: gradient,
                        color: gradient == null ? (color ?? p.accent) : null,
                        borderRadius: Radii.pillAll,
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Строка освоенности одного раздела источника: подпись, полоса, доля.
///
/// Цвет задаётся вызывающим, а не берётся из акцента: список из десяти
/// строк в один цвет читался бы как сплошная стена.
class MasteryRow extends StatelessWidget {
  const MasteryRow({
    super.key,
    required this.title,
    required this.subtitle,
    required this.value,
    required this.color,
    required this.trailing,
    this.onTap,
  });

  final String title;
  final String subtitle;
  final double value;
  final Color color;
  final String trailing;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final content = Padding(
      padding: const EdgeInsets.symmetric(vertical: Gap.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: AppType.bodyStrong.copyWith(color: p.ink),
                ),
              ),
              const SizedBox(width: Gap.md),
              Text(trailing, style: AppType.label.copyWith(color: p.inkMuted)),
            ],
          ),
          const SizedBox(height: Gap.sm),
          ProgressBar(
            value: value,
            height: 8,
            gradient: LinearGradient(
              colors: [color.withValues(alpha: 0.65), color],
            ),
          ),
          const SizedBox(height: Gap.sm),
          Text(subtitle, style: AppType.caption.copyWith(color: p.inkFaint)),
        ],
      ),
    );

    if (onTap == null) return content;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: Radii.controlAll,
        child: content,
      ),
    );
  }
}
