import 'package:flutter/material.dart';

import '../design/palette.dart';
import '../design/tokens.dart';
import '../design/typography.dart';
import 'leading_glyph.dart';

/// Наименьшая цель нажатия — 48 dp (WCAG 2.2 SC 2.5.8 и руководство Android
/// по доступности).
///
/// Это **предел снизу**, а не высота: высоты в приложении не фиксируются,
/// иначе при масштабе шрифта 200 % содержимое обрезалось бы вместо того,
/// чтобы вырасти.
///
/// Величина живёт здесь, а не у каждого, кому понадобилась: цель нажатия у
/// строки настроек, у возврата в шапке и у значка-действия — одно и то же
/// число, а два места для одного числа расходятся молча.
const double minTouchTarget = 48;

/// Плоскость с заливкой и волосяной границей.
///
/// Используется только там, где блок действительно нужно отделить от фона.
/// Внутри списков предпочтительнее [AppRow] с разделителями: вложенные
/// рамки и скругления дробят экран и мешают читать.
class Surface extends StatelessWidget {
  const Surface({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(Gap.lg),
    this.onTap,
    this.radius = Radii.surfaceAll,
    this.color,
    this.borderColor,
  });

  final Widget child;
  final EdgeInsets padding;
  final VoidCallback? onTap;
  final BorderRadius radius;
  final Color? color;
  final Color? borderColor;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final decorated = DecoratedBox(
      decoration: BoxDecoration(
        color: color ?? p.surface,
        borderRadius: radius,
        border: Border.all(color: borderColor ?? p.hairline),
      ),
      child: Padding(padding: padding, child: child),
    );

    if (onTap == null) return decorated;
    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        borderRadius: radius,
        splashColor: p.accent.withValues(alpha: 0.06),
        highlightColor: p.accent.withValues(alpha: 0.04),
        child: decorated,
      ),
    );
  }
}

/// Строка списка без рамки и скругления.
///
/// Списки строятся из таких строк с разделителем [rowDivider]: элементы
/// выстраиваются по одной левой линии, экран не распадается на карточки.
class AppRow extends StatelessWidget {
  const AppRow({
    super.key,
    required this.child,
    this.onTap,
    this.padding = const EdgeInsets.symmetric(horizontal: 16, vertical: Gap.lg),
  });

  final Widget child;
  final VoidCallback? onTap;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final content = Padding(padding: padding, child: child);
    if (onTap == null) return content;

    return Material(
      color: Colors.transparent,
      child: InkWell(
        onTap: onTap,
        splashColor: p.accent.withValues(alpha: 0.06),
        highlightColor: p.accent.withValues(alpha: 0.04),
        child: content,
      ),
    );
  }
}

/// Разделитель между строками списка: тонкая линия с отступом слева,
/// чтобы она не перечёркивала иконку или бейдж.
Widget rowDivider(BuildContext context, {double indent = 16}) => Divider(
  height: 1,
  thickness: 1,
  indent: indent,
  color: context.palette.hairline,
);

/// Прописной заголовок секции.
class SectionLabel extends StatelessWidget {
  const SectionLabel(this.text, {super.key, this.trailing});

  final String text;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Padding(
      padding: const EdgeInsets.only(bottom: Gap.md),
      child: Row(
        children: [
          Expanded(
            child: Text(
              text.toUpperCase(),
              style: AppType.overline.copyWith(color: p.inkFaint),
            ),
          ),
          ?trailing,
        ],
      ),
    );
  }
}

/// Шапка экрана: возврат, заголовок с подзаголовком и действия — **одним
/// рядом**.
///
/// Заведена нарядом 295. До неё возврат стоял своей строкой над заголовком
/// («Настройки», подэкраны настроек), и экран тратил две вертикальные полосы
/// там, где нужна одна: на телефоне это отъедало заметную часть первого
/// вида, а заголовок оказывался ниже кнопки — то есть не там, где начинается
/// чтение.
///
/// Применяется так: `ScreenHeader(title: 'Практика', subtitle: …,
/// onBack: () => Navigator.of(context).pop(), actions: […])`. Обе
/// необязательные части выключаются отсутствием довода — у корневых
/// экранов нижнего меню возврата нет, у экрана без действий пуст
/// `actions`.
///
/// Порядок чтения — «назад → заголовок → действия», и он задан порядком в
/// ряду, а не подсказками службам доступности: расхождение между видимым
/// порядком и читаемым сбивает сильнее, чем отсутствие подсказок.
///
/// Высоты здесь нигде не зафиксированы: при масштабе шрифта 200 % заголовок
/// переносится, ряд растёт, и ничего не обрезается.
class ScreenHeader extends StatelessWidget {
  const ScreenHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.onBack,
    this.actions = const [],
  });

  final String title;

  /// Вторая строка под заголовком: счёт, состояние, пояснение. `null` или
  /// пустая строка — строки нет вовсе.
  final String? subtitle;

  /// Что делает возврат. `null` — кнопки возврата на экране нет: так у
  /// корневых экранов нижнего меню.
  ///
  /// У донора здесь стоял не довод, а маршрут: там приложение живёт на
  /// маршрутизаторе, экран открывается по глубокой ссылке, и стека под
  /// ним может не быть вовсе — `pop()` оказался бы мёртвым нажатием, и
  /// решение «куда возвращаться, если возвращаться некуда» принималось
  /// в одном месте. Здесь маршрутизатора нет: экран всегда открыт
  /// `Navigator.push` поверх своего раздела, стек под ним есть по
  /// построению, и мёртвому нажатию взяться неоткуда. Довод не стёрт, а
  /// исправлен: появится маршрутизатор — вернётся и маршрут.
  final VoidCallback? onBack;

  /// Значки-действия справа. Собираются из [ScreenHeaderAction], чтобы цель
  /// нажатия и подпись для служб доступности не заводились заново на каждом
  /// экране.
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final sub = subtitle;
    final back = onBack;
    return Padding(
      padding: const EdgeInsets.only(top: Gap.sm, bottom: Gap.lg),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.center,
        children: [
          if (back != null) ...[
            _BackAction(onBack: back),
            const SizedBox(width: Gap.md),
          ],
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                // Заголовок объявлен заголовком для служб доступности:
                // иначе экран читается как ровный поток текста, и понять,
                // где он начинается, на слух нельзя.
                Semantics(
                  header: true,
                  child: Text(
                    title,
                    style: AppType.display.copyWith(color: p.ink),
                  ),
                ),
                if (sub != null && sub.isNotEmpty)
                  Text(sub, style: AppType.caption.copyWith(color: p.inkFaint)),
              ],
            ),
          ),
          if (actions.isNotEmpty) ...[
            const SizedBox(width: Gap.md),
            ...actions,
          ],
        ],
      ),
    );
  }
}

/// Крупный заголовок экрана в потоке контента — [ScreenHeader] без возврата.
///
/// Остаётся ради экранов, которым нечего ставить слева и справа, и своего
/// расклада не имеет: дублирующая раскладка заголовка разъехалась бы с
/// шапкой по отступам, и разъезд этот врач увидел бы раньше нас.
class ScreenTitle extends StatelessWidget {
  const ScreenTitle({
    super.key,
    required this.title,
    this.subtitle,
    this.trailing,
  });

  final String title;
  final String? subtitle;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) =>
      ScreenHeader(title: title, subtitle: subtitle, actions: [?trailing]);
}

/// Возврат в шапке экрана.
///
/// Системная кнопка «назад» на Android есть всегда, но видимая дорога на
/// экране нужна отдельно: экран мог открыться значком в шапке соседнего, и
/// вернуться врач хочет туда же, куда смотрел секунду назад.
class _BackAction extends StatelessWidget {
  const _BackAction({required this.onBack});

  final VoidCallback onBack;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return ScreenHeaderAction(
      icon: Icons.arrow_back,
      label: 'Назад',
      color: p.ink,
      onTap: onBack,
    );
  }
}

/// Значок-действие в шапке экрана: цель [minTouchTarget] и подпись для
/// служб доступности.
///
/// Подпись обязательна, а не желательна: значок без подписи остаётся для
/// незрячего врача просто «кнопкой».
class ScreenHeaderAction extends StatelessWidget {
  const ScreenHeaderAction({
    super.key,
    required this.icon,
    required this.label,
    required this.onTap,
    this.color,
  });

  final IconData icon;

  /// Одна строка и для подсказки под пальцем, и для служб доступности:
  /// два текста об одном действии расходятся молча.
  final String label;

  final VoidCallback onTap;

  /// `null` — приглушённые чернила. Задаётся там, где шапка своего цвета:
  /// на тёмной зоне «Прогресса» обычный цвет значков не читается.
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Tooltip(
      message: label,
      child: Semantics(
        button: true,
        label: label,
        child: InkResponse(
          onTap: onTap,
          radius: minTouchTarget / 2,
          child: SizedBox(
            width: minTouchTarget,
            height: minTouchTarget,
            child: Center(
              child: Icon(icon, size: 22, color: color ?? p.inkMuted),
            ),
          ),
        ),
      ),
    );
  }
}

/// Компактная метка: код, статус, счётчик.
class Pill extends StatelessWidget {
  const Pill({
    super.key,
    required this.label,
    this.icon,
    this.color,
    this.background,
    this.dense = false,
  });

  final String label;
  final Widget? icon;
  final Color? color;
  final Color? background;
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final fg = color ?? p.inkMuted;
    return Container(
      padding: EdgeInsets.symmetric(
        horizontal: dense ? Gap.sm : Gap.md,
        vertical: dense ? 2 : 4,
      ),
      decoration: BoxDecoration(
        color: background ?? p.surfaceMuted,
        borderRadius: Radii.controlAll,
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            IconTheme(
              data: IconThemeData(size: dense ? 12 : 14, color: fg),
              child: icon!,
            ),
            const SizedBox(width: Gap.xs),
          ],
          // Гибким, а не как есть: у донора подписи таблеток короткие и
          // заданы в коде, а здесь в них попадают слова источника —
          // «дифференциальный диагноз» длиннее узкого экрана при крупном
          // системном шрифте. Без этого Flutter рисовал бы полосу отказа
          // вместо многоточия. Ряд при этом остаётся по содержимому:
          // Flexible сжимает только там, где места действительно нет.
          Flexible(
            child: Text(
              label,
              overflow: TextOverflow.ellipsis,
              style: (dense ? AppType.caption : AppType.label).copyWith(
                color: fg,
                fontWeight: FontWeight.w600,
                fontVariations: const [FontVariation('wght', 600)],
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// Пустое состояние: иконка, заголовок, пояснение.
class EmptyState extends StatelessWidget {
  const EmptyState({
    super.key,
    required this.icon,
    required this.title,
    this.description,
    this.action,
  });

  final Widget icon;
  final String title;
  final String? description;

  /// Выход из положения, если он есть. Пустой экран посреди начатой
  /// работы — тупик: остаётся кнопка «назад», а она бросает и всё
  /// остальное, что врач успел сделать.
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(Gap.xl),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            IconTheme(
              data: IconThemeData(size: 28, color: p.inkFaint),
              child: icon,
            ),
            const SizedBox(height: Gap.md),
            Text(
              title,
              style: AppType.titleS.copyWith(color: p.ink),
              textAlign: TextAlign.center,
            ),
            if (description != null) ...[
              const SizedBox(height: Gap.xs),
              Text(
                description!,
                style: AppType.caption.copyWith(color: p.inkFaint),
                textAlign: TextAlign.center,
              ),
            ],
            if (action != null) ...[const SizedBox(height: Gap.xl), action!],
          ],
        ),
      ),
    );
  }
}

/// Тёмная зона в начале экрана — приём донора, и он там не украшение.
///
/// Экран, где врач смотрит на себя (звание, опыт, знаки), отделён от
/// остального приложения не заголовком, а сменой основы: тёплая бумага
/// уступает место глубокой воде. Это тот же разворот, что в печати, где
/// разделитель тома делается вывороткой, — и работает он по той же
/// причине: смену фона глаз замечает раньше, чем читает слова.
///
/// Зона заходит под системную полосу состояния намеренно, поэтому верхний
/// отступ считается от [MediaQuery.paddingOf], а не задан числом: зона,
/// начатая ниже выреза, выглядит наклейкой поверх экрана.
///
/// Чернила здесь свои ([AppPalette.heroInk] и [AppPalette.heroInkMuted]):
/// обычный цвет текста на этой подложке не читается, и подставлять его
/// «пока сойдёт» нельзя — не читается он ровно у того, кому темно.
class HeroZone extends StatelessWidget {
  const HeroZone({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.fromLTRB(20, Gap.md, 20, Gap.xl),
  });

  final Widget child;

  /// Отступы внутри зоны. Верхний складывается с высотой системной
  /// полосы, а не заменяет её.
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return DecoratedBox(
      decoration: BoxDecoration(gradient: p.heroGradient),
      child: Padding(
        padding: padding.copyWith(
          top: padding.top + MediaQuery.paddingOf(context).top,
        ),
        child: child,
      ),
    );
  }
}

/// Плитка на тёмной зоне: полупрозрачное стекло с волосяной границей.
///
/// Заливать её [AppPalette.surface] нельзя — светлая карточка на тёмной
/// основе разрывает зону на куски. Стекло же берёт цвет у того, что под
/// ним, и зона остаётся одной плоскостью с выступами.
class HeroTile extends StatelessWidget {
  const HeroTile({
    super.key,
    required this.child,
    this.padding = const EdgeInsets.all(Gap.lg),
  });

  final Widget child;
  final EdgeInsets padding;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return DecoratedBox(
      decoration: BoxDecoration(
        color: p.heroGlass,
        borderRadius: Radii.surfaceAll,
        border: Border.all(color: p.heroHairline),
      ),
      child: Padding(padding: padding, child: child),
    );
  }
}

/// Короткое сообщение под шапкой: что сейчас произошло.
///
/// Не [EmptyState]: то занимает экран и говорит «здесь ничего нет», а это
/// строка над содержимым — «набор убран», «сеть не отвечает». Экран при
/// этом продолжает работать, и отнимать его целиком ради строки нельзя.
///
/// Живёт здесь, а не у каждого экрана: своя плашка на каждом экране
/// разъезжается с соседними по отступу и радиусу — а разъезд этот врач
/// видит раньше нас.
class NoteBanner extends StatelessWidget {
  const NoteBanner({super.key, required this.text, this.icon});

  final String text;

  /// Значок слева. Задаётся там, где сообщение про отказ: у отказа и у
  /// сделанного разный вес, и различать их одним цветом текста мало.
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final glyph = icon;
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(Gap.md),
      decoration: BoxDecoration(
        color: p.surfaceMuted,
        borderRadius: Radii.controlAll,
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (glyph != null) ...[
            LeadingGlyph(
              lineStyle: AppType.body,
              child: Icon(glyph, size: 16, color: p.inkMuted),
            ),
            const SizedBox(width: Gap.sm),
          ],
          Expanded(
            child: Text(text, style: AppType.body.copyWith(color: p.ink)),
          ),
        ],
      ),
    );
  }
}
