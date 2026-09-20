/// Движение как обратная связь: набегающие счётчики, пружинное появление
/// и бегущий блик.
///
/// Залп конфетти у донора остался у донора: пускать его здесь пока
/// нечему — вершины, за которой он даётся, в этом приложении ещё нет, а
/// виджет, которого никто не зовёт, выглядит работающим и не работает.
///
/// Общее правило файла — анимация запускается один раз при появлении и
/// заканчивается. Ничего не крутится в бесконечном цикле, кроме [Shimmer],
/// который включают только на действительно активной полосе: постоянное
/// движение на экране статистики быстро начинает раздражать.
library;

import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../design/tokens.dart';

/// Число, набегающее от нуля до [value].
///
/// Стиль передаётся снаружи и должен быть моноширинным по цифрам, иначе
/// ширина по ходу счёта поедет и соседние элементы задёргаются.
class CountUp extends StatelessWidget {
  const CountUp({
    super.key,
    required this.value,
    required this.style,
    this.suffix,
    this.suffixStyle,
    this.duration = Motion.slow,
    this.delay = Duration.zero,
  });

  final int value;
  final TextStyle style;

  /// Приписка вплотную к числу: «%», «дн.». Всегда мельче самого числа.
  final String? suffix;
  final TextStyle? suffixStyle;
  final Duration duration;
  final Duration delay;

  @override
  Widget build(BuildContext context) {
    return _Delayed(
      delay: delay,
      // До начала счёта показываем ноль, а не пустоту: иначе блок
      // схлопывается и вся карточка прыгает, когда число появится.
      placeholder: _text(0),
      child: TweenAnimationBuilder<double>(
        tween: Tween(begin: 0, end: value.toDouble()),
        duration: duration,
        curve: Motion.enter,
        builder: (context, v, _) => _text(v.round()),
      ),
    );
  }

  Widget _text(int shown) => Text.rich(
    TextSpan(
      text: '$shown',
      children: [
        if (suffix != null) TextSpan(text: suffix, style: suffixStyle ?? style),
      ],
    ),
    style: style,
  );
}

/// Появление с пружиной: сдвиг снизу, лёгкий перелёт по масштабу.
///
/// Списки собираются из таких блоков с нарастающим [delay] — получается
/// каскад, в котором глаз успевает пройти экран сверху вниз.
class PopIn extends StatefulWidget {
  const PopIn({
    super.key,
    required this.child,
    this.delay = Duration.zero,
    this.offset = 14,
  });

  final Widget child;
  final Duration delay;

  /// Насколько блок приподнят снизу в начале анимации.
  final double offset;

  /// Каскад: `index`-й элемент стартует на [Motion.stagger] позже
  /// предыдущего, но не дальше [Motion.staggerCap] шагов от начала.
  static Duration stagger(int index) =>
      Motion.stagger * math.min(index, Motion.staggerCap);

  @override
  State<PopIn> createState() => _PopInState();
}

class _PopInState extends State<PopIn> with SingleTickerProviderStateMixin {
  // Контроллер создаётся сразу, а не лениво: `late final` инициализировался
  // бы при первом обращении, и если это обращение — `dispose`, контроллер
  // полез бы за TickerMode в уже отсоединённое поддерево.
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(vsync: this, duration: Motion.base);
    unawaited(_start());
  }

  Future<void> _start() async {
    if (widget.delay > Duration.zero) {
      await Future<void>.delayed(widget.delay);
      if (!mounted) return;
    }
    unawaited(_controller.forward());
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, child) {
        final t = Motion.spring.transform(_controller.value);
        return Opacity(
          // Прозрачность гасим быстрее сдвига и без перелёта: значение
          // из easeOutBack выходит за 0..1 и Opacity на нём падает.
          opacity: _controller.value.clamp(0.0, 1.0),
          child: Transform.translate(
            offset: Offset(0, widget.offset * (1 - t)),
            child: child,
          ),
        );
      },
      child: widget.child,
    );
  }
}

/// Блик, бегущий по активной полосе прогресса.
///
/// [enabled] выключает анимацию вместе с самим эффектом: полоса без
/// прогресса не должна мерцать вхолостую.
class Shimmer extends StatefulWidget {
  const Shimmer({super.key, required this.child, this.enabled = true});

  final Widget child;
  final bool enabled;

  @override
  State<Shimmer> createState() => _ShimmerState();
}

class _ShimmerState extends State<Shimmer> with SingleTickerProviderStateMixin {
  late final AnimationController _controller;

  @override
  void initState() {
    super.initState();
    _controller = AnimationController(vsync: this, duration: Motion.shimmer);
    if (widget.enabled) unawaited(_controller.repeat());
  }

  @override
  void didUpdateWidget(Shimmer old) {
    super.didUpdateWidget(old);
    if (widget.enabled == old.enabled) return;
    if (widget.enabled) {
      unawaited(_controller.repeat());
    } else {
      _controller.stop();
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.enabled) return widget.child;
    // Системное «уменьшить движение»: блик — движение ради движения.
    if (MediaQuery.maybeDisableAnimationsOf(context) ?? false) {
      return widget.child;
    }
    return AnimatedBuilder(
      animation: _controller,
      builder: (context, child) => ShaderMask(
        blendMode: BlendMode.srcATop,
        shaderCallback: (rect) {
          // Блик уезжает далеко за края, чтобы между проходами была пауза,
          // а не непрерывное мельтешение.
          final travel = rect.width * 2.4;
          final x = -rect.width * 0.7 + travel * _controller.value;
          return const LinearGradient(
            begin: Alignment.centerLeft,
            end: Alignment.centerRight,
            colors: [Color(0x00FFFFFF), Color(0x59FFFFFF), Color(0x00FFFFFF)],
            stops: [0.0, 0.5, 1.0],
          ).createShader(
            Rect.fromLTWH(x, rect.top, rect.width * 0.45, rect.height),
          );
        },
        child: child,
      ),
      child: widget.child,
    );
  }
}

/// Тактильный отклик по значимым событиям.
///
/// Обёртка, а не прямые вызовы [HapticFeedback]: на десктопе и в тестах
/// канал вибрации отсутствует, и отказ платформы не должен всплывать
/// исключением посреди разбора задачи.
abstract final class Haptics {
  static void success() => _fire(HapticFeedback.mediumImpact);
  static void failure() => _fire(HapticFeedback.vibrate);
  static void tick() => _fire(HapticFeedback.selectionClick);
  static void celebrate() => _fire(HapticFeedback.heavyImpact);

  /// Вершина: два удара подряд.
  ///
  /// Отклик у награды — часть её веса, и одинаковый удар на бронзу и на
  /// звание стирает разницу между ними ровно там, где телефон уже в
  /// руке. Двойной удар различим вслепую, в отличие от оттенков силы, —
  /// поэтому вершина отмечена ритмом, а не громкостью.
  static void triumph() {
    _fire(HapticFeedback.heavyImpact);
    unawaited(
      Future<void>.delayed(
        const Duration(milliseconds: 130),
        () => _fire(HapticFeedback.heavyImpact),
      ),
    );
  }

  static void _fire(Future<void> Function() effect) {
    unawaited(effect().catchError((Object _) {}));
  }
}

/// Задержка появления: до срока показывается [placeholder].
class _Delayed extends StatefulWidget {
  const _Delayed({
    required this.delay,
    required this.child,
    required this.placeholder,
  });

  final Duration delay;
  final Widget child;
  final Widget placeholder;

  @override
  State<_Delayed> createState() => _DelayedState();
}

class _DelayedState extends State<_Delayed> {
  late bool _ready = widget.delay == Duration.zero;

  @override
  void initState() {
    super.initState();
    if (_ready) return;
    unawaited(
      Future<void>.delayed(widget.delay, () {
        if (mounted) setState(() => _ready = true);
      }),
    );
  }

  @override
  Widget build(BuildContext context) =>
      _ready ? widget.child : widget.placeholder;
}
