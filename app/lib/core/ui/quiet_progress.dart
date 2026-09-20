import 'dart:async';

import 'package:flutter/material.dart';

import '../design/palette.dart';
import '../design/tokens.dart';
import '../design/typography.dart';

/// Тонкая отметка «идёт обновление»: подпись и волосяная полоска под ней.
///
/// Перенесена от донора взамен полноэкранных кружков. Приложение обязано
/// работать без сети: всё, что оно умеет показать с диска, оно показывает
/// немедленно, а обновление идёт поверх — сбоку от внимания. Кружок на весь
/// экран объявляет работу невозможной, пока ответит сервер, — а сервер может
/// не ответить никогда, и врач в метро оставался бы с крутящимся колесом там,
/// где у него на устройстве лежит всё нужное.
///
/// **Отметка ничего не утверждает.** Она не говорит «задач нет» и не говорит
/// «ошибка»: она говорит, что идёт проверка, и ею можно пренебречь.
///
/// # Почему у отметки две задержки
///
/// Отметка, включающаяся прямо по состоянию провайдера, мигает: чтение с
/// диска укладывается в десяток миллисекунд, и полоска успевает вспыхнуть на
/// один-два кадра. Мигание хуже честного ожидания — глаз ловит движение и
/// ищет, что изменилось, а менять нечего. Поэтому:
///
/// * [appearAfter] — обновление, кончившееся быстрее, не показывается вовсе;
/// * [minVisible] — показанное держится, даже если обновление уже кончилось.
///
/// Обе задержки — про глаз, а не про сеть, и потому измеряются от появления
/// отметки, а не от начала обращения.
class UpdatingLine extends StatefulWidget {
  const UpdatingLine({super.key, required this.updating, required this.label});

  /// Идёт ли обновление прямо сейчас.
  final bool updating;

  /// Что обновляется — словами врача: «Задачи обновляются», а не «loading».
  final String label;

  /// Быстрее этого обновление не показывается вовсе.
  static const appearAfter = Duration(milliseconds: 400);

  /// Показанное держится не меньше этого.
  static const minVisible = Duration(milliseconds: 900);

  /// Высота полоски. Литералом, а не токеном: токены задают ритм отступов, а
  /// это толщина линии из чужой спецификации (M3 `progress-indicators`), и
  /// подгонять её под сетку значило бы разойтись с источником.
  static const double trackHeight = 2;

  @override
  State<UpdatingLine> createState() => _UpdatingLineState();
}

class _UpdatingLineState extends State<UpdatingLine> {
  bool _shown = false;
  DateTime? _shownAt;
  Timer? _appear;
  Timer? _hold;

  @override
  void initState() {
    super.initState();
    _sync();
  }

  @override
  void didUpdateWidget(covariant UpdatingLine old) {
    super.didUpdateWidget(old);
    if (old.updating != widget.updating) _sync();
  }

  @override
  void dispose() {
    _appear?.cancel();
    _hold?.cancel();
    super.dispose();
  }

  void _sync() {
    if (widget.updating) {
      // Начавшееся снова обновление отменяет гашение: полоска, погасшая и
      // тут же зажжённая, — это и есть мигание.
      _hold?.cancel();
      _hold = null;
      if (_shown || _appear != null) return;
      _appear = Timer(UpdatingLine.appearAfter, () {
        _appear = null;
        if (!mounted || !widget.updating) return;
        setState(() {
          _shown = true;
          _shownAt = DateTime.now();
        });
      });
      return;
    }

    _appear?.cancel();
    _appear = null;
    if (!_shown || _hold != null) return;
    final shownAt = _shownAt;
    final left = shownAt == null
        ? Duration.zero
        : UpdatingLine.minVisible - DateTime.now().difference(shownAt);
    if (left <= Duration.zero) {
      setState(() => _shown = false);
      return;
    }
    _hold = Timer(left, () {
      _hold = null;
      if (!mounted || widget.updating) return;
      setState(() => _shown = false);
    });
  }

  @override
  Widget build(BuildContext context) {
    if (!_shown) return const SizedBox.shrink();
    final p = context.palette;
    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Semantics(
        // Не `liveRegion`: озвучивать проверку обновлений посреди чтения —
        // то же вмешательство, что и кружок на весь экран, только на слух.
        label: widget.label,
        child: ExcludeSemantics(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                widget.label,
                style: AppType.caption.copyWith(color: p.inkFaint),
              ),
              const SizedBox(height: Gap.sm),
              ClipRRect(
                borderRadius: Radii.pillAll,
                child: LinearProgressIndicator(
                  minHeight: UpdatingLine.trackHeight,
                  backgroundColor: p.surfaceMuted,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
