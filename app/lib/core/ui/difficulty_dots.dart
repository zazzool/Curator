import 'package:flutter/material.dart';

import '../design/palette.dart';

/// Сложность задачи точками — спокойнее звёзд и не тянет на себя внимание.
///
/// Виджет ничего не знает о шкале: он рисует `level` из `max`. Границы
/// шкалы и подпись к точкам даёт вызывающий — иначе оформление потянуло
/// бы за собой игровую механику. Открывать точкам нечего: трудность —
/// свойство задачи и признак отбора, но не запрет.
class DifficultyDots extends StatelessWidget {
  // Точек четыре: шкала видна целиком, и «три из четырёх» отличимо от
  // «потолка».
  const DifficultyDots({super.key, required this.level, this.max = 4});

  final int level;
  final int max;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 1; i <= max; i++) ...[
          // Зазор только между точками: хвостовой отступ сдвигал бы всю
          // группу влево там, где её ставят к правому краю.
          if (i > 1) const SizedBox(width: 3),
          Container(
            width: 6,
            height: 6,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: i <= level ? p.inkMuted : p.hairline,
            ),
          ),
        ],
      ],
    );
  }
}
