/// Критерии рубрики на экране.
///
/// # Почему не просто список абзацев
///
/// Критерии диагноза — это не проза, а структура: у каждого есть род
/// («обязательный», «исключение», «длительность»), обозначение в
/// первоисточнике («G1», «абз. 2») и место, откуда он взят. Показанные
/// подряд одинаковым кеглем, они читаются как один длинный абзац, и врач
/// теряет главное — что из этого обязательно, а что исключает диагноз.
/// Поэтому род ведёт группировку, задаёт знак и цвет, а обозначение стоит
/// отдельно от текста.
///
/// # Род спрашивается у источника, а не угадывается
///
/// Слово рода приходит из самого источника и показывается как есть.
/// Соответствие «слово рода — знак» ниже — это подсказка показа, а не
/// ветка по источнику: незнакомое слово получает общий знак и свою группу,
/// и ничего от этого не ломается. Ветка «если это МКБ» здесь была бы
/// дефектом: справочников много, и все они одинаково чужие.
///
/// # Обозначение, которое оказалось меткой, становится ссылкой
///
/// У дифференциального диагноза обозначение — это метка другой рубрики
/// («F41.2»), и одна она врачу не говорит ничего: чтобы узнать, с чем же
/// путают, он должен помнить код наизусть или уйти искать его в дереве и
/// потерять место, на котором читал. Поэтому обозначение, совпавшее с
/// меткой того же источника, показывается вместе с названием и открывает
/// ту рубрику по нажатию.
///
/// Совпадение ищется по справочнику на устройстве, а не по виду метки:
/// «если это похоже на код МКБ» — тот самый дефект. Не нашлось — остаётся
/// обычное обозначение, и ничего не ломается.
///
/// # Обозначение не показывается дважды
///
/// В выгрузках обозначение сплошь и рядом повторено в начале текста
/// («G1. Длительность не менее двух недель»). Показанное и значком, и
/// первым словом, оно выглядит опечаткой набора — врач видит «G1 G1.».
/// Поэтому начало текста сверяется с обозначением и срезается.
library;

import 'package:flutter/material.dart';

import '../text/plural.dart';
import '../text/prose.dart';
import 'model.dart';

/// Знак рода положения.
///
/// Соответствие открытое и нарочно неполное: слово, которого здесь нет,
/// получает общий знак. Закрытый словарь здесь означал бы, что источник со
/// своими словами показать нельзя, — а источник любой.
IconData statementIcon(String kind) {
  final word = kind.trim().toLowerCase();
  if (word.isEmpty) return Icons.article_outlined;
  bool has(List<String> roots) => roots.any(word.contains);

  if (has(['обязат', 'основн', 'критер'])) return Icons.check_circle_outline;
  if (has(['исключ', 'не долж', 'запрет'])) return Icons.block_outlined;
  if (has(['длительн', 'срок', 'продолж'])) return Icons.schedule_outlined;
  if (has(['дополнит', 'факульт', 'необязат'])) return Icons.add_circle_outline;
  if (has(['тяжест', 'степен', 'выражен'])) return Icons.bar_chart_outlined;
  if (has(['примеч', 'коммент', 'поясн'])) return Icons.info_outline;
  // «Клинические описания» — самый частый род у МКБ-10 (561 блок из 765),
  // и общий знак статьи у него значил бы, что знака нет у большинства
  // критериев справочника.
  if (has(['описан', 'картина'])) return Icons.menu_book_outlined;
  if (has(['обязанн', 'требован', 'предпис'])) return Icons.gavel_outlined;
  if (has(['диффер', 'путают', 'отлич'])) return Icons.compare_arrows_outlined;
  return Icons.article_outlined;
}

/// Срезает обозначение, повторённое в начале текста.
///
/// Срезается только точное повторение в самом начале и только вместе с
/// разделителем: «G1» внутри фразы — это ссылка на другой критерий, и
/// трогать её нельзя.
String withoutDesignation(String body, String designation) {
  final mark = designation.trim();
  if (mark.isEmpty) return body;
  final text = body.trimLeft();
  if (!text.toLowerCase().startsWith(mark.toLowerCase())) return body;

  final rest = text.substring(mark.length);
  if (rest.isEmpty) return body;
  // Без разделителя это не повтор обозначения, а начало слова:
  // «Гипотимия» начинается с «Г», и срезать её по обозначению «Г» значило
  // бы съесть первую букву критерия.
  if (!_separator(rest[0])) return body;

  var out = rest;
  while (out.isNotEmpty && _separator(out[0])) {
    out = out.substring(1);
  }
  return out.isEmpty ? body : out;
}

/// Знак между обозначением и текстом: пробел или знак препинания.
bool _separator(String ch) => ch.trim().isEmpty || '.):-—–'.contains(ch);

/// Положения единицы, разложенные по родам.
///
/// Порядок родов — порядок первого появления, а не алфавит: источник
/// расставил критерии сам, и переставлять их по названию рода значит
/// читать источник не в том порядке, в каком он написан.
List<MapEntry<String, List<RefStatement>>> byKind(List<RefStatement> all) {
  final order = <String>[];
  final groups = <String, List<RefStatement>>{};
  for (final one in all) {
    final kind = one.kind.trim();
    if (!groups.containsKey(kind)) {
      order.add(kind);
      groups[kind] = [];
    }
    groups[kind]!.add(one);
  }
  return [for (final kind in order) MapEntry(kind, groups[kind]!)];
}

/// Полоска-сводка: чего и сколько в рубрике.
///
/// Она отвечает на вопрос, который врач задаёт до чтения: много ли тут
/// всего и чего именно. Без неё он узнаёт это, пролистав рубрику до конца.
class KindStrip extends StatelessWidget {
  const KindStrip({super.key, required this.statements, this.children = 0});

  final List<RefStatement> statements;

  /// Сколько внутри вложенных единиц. Ноль — рубрика конечная.
  final int children;

  @override
  Widget build(BuildContext context) {
    final groups = byKind(statements);
    if (groups.isEmpty && children == 0) return const SizedBox.shrink();

    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        for (final group in groups)
          _Chip(
            icon: statementIcon(group.key),
            text: group.key.isEmpty
                ? '${group.value.length}'
                : '${group.key} · ${group.value.length}',
          ),
        if (children > 0)
          _Chip(icon: Icons.account_tree_outlined, text: 'внутри: $children'),
      ],
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень: полоска лежит на странице, а не
        // висит над ней.
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 15, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 6),
          Text(text, style: theme.textTheme.labelMedium),
        ],
      ),
    );
  }
}

/// Список положений единицы, разложенный по родам.
class CriteriaList extends StatelessWidget {
  const CriteriaList({
    super.key,
    required this.statements,
    required this.statementWord,
    this.links = const {},
    this.onLink,
  });

  final List<RefStatement> statements;

  /// Метка рубрики — её название, для обозначений, оказавшихся метками.
  final Map<String, String> links;

  /// Что делать по нажатию на такую ссылку. Не задано — ссылка остаётся
  /// обычным обозначением: экран, не умеющий открыть рубрику, не должен
  /// притворяться, что умеет.
  final void Function(String label)? onLink;

  /// Как источник зовёт положение: «критерий», «пункт», «положение».
  /// Показывается как есть — слово «положение» у МКБ-10 сказало бы врачу,
  /// что приложение не знает, что показывает.
  final String statementWord;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final groups = byKind(statements);
    if (groups.isEmpty) return const SizedBox.shrink();

    // Род показывается заголовком только тогда, когда родов больше одного
    // или он назван: единственная безымянная группа — это просто список,
    // и заголовок над ним был бы шумом.
    final titled = groups.length > 1 || groups.first.key.isNotEmpty;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (var g = 0; g < groups.length; g++) ...[
          if (g > 0) const SizedBox(height: 20),
          if (titled)
            Padding(
              padding: const EdgeInsets.only(bottom: 10),
              child: Row(
                children: [
                  Icon(
                    statementIcon(groups[g].key),
                    size: 18,
                    color: theme.colorScheme.primary,
                  ),
                  const SizedBox(width: 8),
                  Text(
                    // Безымянный род зовётся словом самого источника —
                    // «критерии» у МКБ-10, «пункты» у приказа. Заголовок
                    // «» посреди названных родов читался бы как пропуск.
                    groups[g].key.isEmpty
                        ? pluralWord(statementWord)
                        : groups[g].key,
                    style: theme.textTheme.titleSmall?.copyWith(
                      color: theme.colorScheme.primary,
                    ),
                  ),
                ],
              ),
            ),
          for (final one in groups[g].value)
            StatementTile(
              one: one,
              statementWord: statementWord,
              linkTitle: links[one.designation] ?? '',
              onLink: onLink,
            ),
        ],
      ],
    );
  }
}

/// Одно положение.
class StatementTile extends StatelessWidget {
  const StatementTile({
    super.key,
    required this.one,
    required this.statementWord,
    this.linkTitle = '',
    this.onLink,
  });

  final RefStatement one;
  final String statementWord;

  /// Название рубрики, если обозначение оказалось её меткой.
  final String linkTitle;
  final void Function(String label)? onLink;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final body = withoutDesignation(one.body, one.designation);

    // Ссылка занимает всю ширину, а не колонку в сорок четыре точки:
    // название рубрики туда не влезает, а ради него ссылка и делается.
    if (linkTitle.isNotEmpty) {
      return Padding(
        padding: const EdgeInsets.only(bottom: 16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _LinkChip(
              label: one.designation,
              title: linkTitle,
              onTap: onLink == null ? null : () => onLink!(one.designation),
            ),
            const SizedBox(height: 6),
            Prose(body, style: theme.textTheme.bodyLarge),
            if (one.placeRef.isNotEmpty) _Place(text: one.placeRef),
          ],
        ),
      );
    }

    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Обозначение стоит колонкой слева: по нему критерий ищут
          // глазами, а внутри текста оно теряется.
          SizedBox(
            width: 44,
            child: one.designation.isEmpty
                ? Icon(
                    statementIcon(one.kind),
                    size: 16,
                    color: theme.colorScheme.outline,
                  )
                : Text(
                    one.designation,
                    style: theme.textTheme.labelLarge?.copyWith(
                      color: theme.colorScheme.primary,
                    ),
                  ),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Prose(body, style: theme.textTheme.bodyLarge),
                if (one.placeRef.isNotEmpty) _Place(text: one.placeRef),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Ссылка на другую рубрику: метка и её название.
class _LinkChip extends StatelessWidget {
  const _LinkChip({required this.label, required this.title, this.onTap});

  final String label;
  final String title;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final chip = Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 6),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень: ссылка лежит в тексте, а не висит
        // над ним.
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(
            Icons.compare_arrows_outlined,
            size: 15,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 8),
          Text(
            label,
            style: theme.textTheme.labelLarge?.copyWith(
              color: theme.colorScheme.primary,
            ),
          ),
          const SizedBox(width: 8),
          Flexible(child: Prose(title, style: theme.textTheme.bodyMedium)),
          if (onTap != null) ...[
            const SizedBox(width: 6),
            Icon(
              Icons.chevron_right,
              size: 16,
              color: theme.colorScheme.outlineVariant,
            ),
          ],
        ],
      ),
    );
    if (onTap == null) return chip;
    return InkWell(
      onTap: onTap,
      borderRadius: BorderRadius.circular(8),
      child: chip,
    );
  }
}

/// Откуда положение взято.
class _Place extends StatelessWidget {
  const _Place({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            Icons.bookmark_border,
            size: 13,
            color: theme.colorScheme.outline,
          ),
          const SizedBox(width: 4),
          Flexible(
            child: Text(
              text,
              style: theme.textTheme.labelSmall?.copyWith(
                color: theme.colorScheme.outline,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
