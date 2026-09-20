/// Примитивы экрана «Настройки»: группа и строка-переход.
///
/// **Второй реализации списка не заводится.** Набор стоит на `Surface`,
/// `AppRow`, `rowDivider` и `SectionLabel` из `core/ui/surface.dart`, а не
/// на `ListTile` рядом с ними: экран настроек, собранный из чужих
/// кирпичей, выглядел бы чужим в своём приложении — другие отступы, другая
/// левая линия, другие разделители.
///
/// У донора здесь есть ещё строка-переключатель и строка опасного
/// действия. Они не перенесены не потому, что не нужны, а потому, что
/// переключать и отменять пока нечего: строка, заведённая впрок, остаётся
/// непроверенной ровно до того дня, когда ею воспользуются. Понадобятся —
/// переносить их надо вместе с их доводами: подпись переключателя в
/// положительном залоге и подтверждение последствием, а не «Вы уверены?».
library;

import 'package:flutter/material.dart';

import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/surface.dart';

/// Наименьшая цель нажатия у строки настроек.
///
/// Число не своё: оно то же, что у возврата и значков в шапке экрана, и
/// потому взято из [minTouchTarget], а не написано здесь заново — два
/// места для одного числа расходятся молча.
const double settingsRowMinHeight = minTouchTarget;

/// Группа настроек: прописной заголовок и строки под одной плоскостью.
///
/// Заголовок обязан говорить, что внутри: «Дополнительно» и «Разное» —
/// прямой антипаттерн, и потому группы называются предметом.
///
/// Разделитель ставится между строками внутри группы, а группы отделяются
/// воздухом: тени в приложении не вводятся.
class SettingsGroup extends StatelessWidget {
  const SettingsGroup({super.key, required this.title, required this.rows});

  final String title;

  /// Строки группы. Пусто — группы нет вовсе: раздел, которому нечего
  /// показать, не должен занимать место обещанием.
  final List<Widget> rows;

  @override
  Widget build(BuildContext context) {
    if (rows.isEmpty) return const SizedBox.shrink();

    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.xxl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SectionLabel(title),
          Surface(
            padding: EdgeInsets.zero,
            // Один `Material` на всю группу, а не по одному на строку:
            // прямое указание документации `SwitchListTile` — на длинном
            // экране своя плоскость у каждой строки стоит дорого. Отсюда
            // же и то, что строки ниже берут `InkWell` напрямую, а не
            // `AppRow.onTap`: тот заводит свой `Material`.
            child: Material(
              color: Colors.transparent,
              // Скругление и обрезка — чтобы отклик нажатия не вылезал за
              // углы плоскости: у крайних строк он иначе рисуется поверх
              // волосяной границы.
              borderRadius: Radii.surfaceAll,
              clipBehavior: Clip.antiAlias,
              child: Column(
                children: [
                  for (var i = 0; i < rows.length; i++) ...[
                    if (i > 0) rowDivider(context),
                    rows[i],
                  ],
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Каркас строки: нажимается вся строка целиком, цель не меньше 48 точек.
///
/// Нажатие только по управляющему элементу — частая ошибка экранов
/// настроек: палец попадает в подпись, и ничего не происходит.
class _SettingsRow extends StatelessWidget {
  const _SettingsRow({required this.onTap, required this.child});

  final VoidCallback? onTap;
  final Widget child;

  @override
  Widget build(BuildContext context) => InkWell(
    onTap: onTap,
    child: ConstrainedBox(
      constraints: const BoxConstraints(minHeight: settingsRowMinHeight),
      child: AppRow(child: child),
    ),
  );
}

/// Две строки пункта: название и текущее значение.
class _RowText extends StatelessWidget {
  const _RowText({required this.title, this.value});

  final String title;
  final String? value;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final second = value;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisAlignment: MainAxisAlignment.center,
      children: [
        Text(title, style: AppType.bodyStrong.copyWith(color: p.ink)),
        // Вторая строка показывает **текущее значение**, а не
        // пересказывает первую. Пересказ — это шум, который читают один
        // раз, а занимает он место навсегда; поэтому нечего показать —
        // нет и строки.
        if (second != null && second.isNotEmpty) ...[
          const SizedBox(height: 2),
          Text(second, style: AppType.caption.copyWith(color: p.inkFaint)),
        ],
      ],
    );
  }
}

/// Строка-переход: на подэкран настроек или в другой раздел приложения.
class SettingsNavRow extends StatelessWidget {
  const SettingsNavRow({
    super.key,
    required this.title,
    required this.onTap,
    this.value,
  });

  /// Название пункта предметом, а не пустым глаголом: «Наборы задач», а не
  /// «Управлять наборами» — «Настроить», «Изменить», «Управлять» и
  /// «Выбрать» не сообщают ничего.
  final String title;

  final VoidCallback onTap;

  /// Текущее значение под названием. `null` — сказать нечего, и второй
  /// строки нет вовсе. Пока состояние не прочитано, здесь тоже `null`:
  /// «наборов нет» на непрочитанном означало бы не «нет», а «не знаем», и
  /// врач принял бы это за правду.
  final String? value;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return _SettingsRow(
      onTap: onTap,
      child: Row(
        children: [
          Expanded(
            child: _RowText(title: title, value: value),
          ),
          const SizedBox(width: Gap.md),
          Icon(Icons.chevron_right, size: 18, color: p.inkFaint),
        ],
      ),
    );
  }
}
