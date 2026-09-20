/// Один источник на устройстве: дерево, поиск, рубрика.
///
/// # Два входа, а не один
///
/// Врач приходит с кодом из карты («F32.1») и со словом из головы
/// («депрессивный эпизод»), и оба входа обязаны работать. Поиск ищет по
/// метке и по названию сразу; дерево остаётся для тех случаев, когда врач
/// не знает, что ищет, а знает только, где это примерно лежит.
///
/// # Всё читается с устройства
///
/// Ни один экран здесь не ходит в сеть. Это не оптимизация: справочник
/// переносили ради отделения без сети, и экран, спрашивающий сервер,
/// обесценил бы перенос целиком.
library;

import 'package:flutter/material.dart';

import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/leading_glyph.dart';
import '../core/ui/search_field.dart';
import '../core/ui/surface.dart';
import '../db/reference_store.dart';
import '../text/plural.dart';
import '../text/prose.dart';
import 'criteria.dart';
import 'model.dart';

/// Корень источника: поиск и верхний уровень дерева.
class SourceScreen extends StatefulWidget {
  const SourceScreen({super.key, required this.store, required this.source});

  final ReferenceStore store;
  final RefSource source;

  @override
  State<SourceScreen> createState() => _SourceScreenState();
}

class _SourceScreenState extends State<SourceScreen> {
  final _query = TextEditingController();

  List<RefUnit> _shown = const [];
  bool _searching = false;
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _roots();
  }

  @override
  void dispose() {
    _query.dispose();
    super.dispose();
  }

  Future<void> _roots() async {
    final units = await widget.store.children(widget.source.slug, '');
    if (!mounted) return;
    setState(() {
      _shown = units;
      _searching = false;
      _loading = false;
    });
  }

  Future<void> _search(String text) async {
    if (text.trim().isEmpty) {
      await _roots();
      return;
    }
    final found = await widget.store.search(widget.source.slug, text);
    if (!mounted) return;
    setState(() {
      _shown = found;
      _searching = true;
      _loading = false;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            children: [
              ScreenHeader(
                title: widget.source.title,
                subtitle: widget.source.edition.isEmpty
                    ? null
                    : widget.source.edition,
                onBack: () => Navigator.of(context).pop(),
              ),
              AppSearchField(
                controller: _query,
                onChanged: _search,
                hintText:
                    'Код или название: '
                    '${widget.source.unitWord.toLowerCase()}',
                onClear: _query.text.isEmpty
                    ? null
                    : () {
                        _query.clear();
                        _search('');
                      },
              ),
              const SizedBox(height: Gap.md),
              if (_searching && _shown.isEmpty)
                const Expanded(
                  child: EmptyState(
                    icon: Icon(Icons.search_off_outlined),
                    title: 'Ничего не нашлось',
                    description:
                        'Попробуйте часть кода или одно слово из названия',
                  ),
                )
              else if (_loading)
                const Expanded(
                  child: Center(child: CircularProgressIndicator()),
                )
              else
                Expanded(
                  child: ListView.separated(
                    padding: const EdgeInsets.only(bottom: Gap.xl),
                    itemCount: _shown.length,
                    // Разделитель между строками, а не рамка у каждой:
                    // список из полусотни карточек распадается на куски,
                    // а строки с общей левой линией читаются одним
                    // столбцом сверху вниз.
                    separatorBuilder: (context, _) =>
                        rowDivider(context, indent: 30),
                    itemBuilder: (_, at) => UnitRow(
                      unit: _shown[at],
                      // В найденном путь показывается, а в дереве нет: в
                      // дереве врач и так знает, где он, а в выдаче
                      // поиска «F32.1» без «Расстройства настроения» над
                      // ним — это код без места.
                      showPath: _searching,
                      statementWord: widget.source.statementWord,
                      onTap: () => openUnit(
                        context,
                        widget.store,
                        widget.source,
                        _shown[at],
                      ),
                    ),
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Открывает рубрику.
void openUnit(
  BuildContext context,
  ReferenceStore store,
  RefSource source,
  RefUnit unit,
) {
  Navigator.of(context).push(
    MaterialPageRoute<void>(
      builder: (_) => UnitScreen(store: store, source: source, unit: unit),
    ),
  );
}

/// Строка списка единиц.
class UnitRow extends StatelessWidget {
  const UnitRow({
    super.key,
    required this.unit,
    required this.statementWord,
    required this.onTap,
    this.showPath = false,
  });

  final RefUnit unit;
  final String statementWord;
  final VoidCallback onTap;
  final bool showPath;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    // Группа и запись отличаются знаком, и отличие взято у источника, а не
    // угадано по виду метки: «если это МКБ» — дефект.
    final group = unit.kind == 'group' || !unit.answerable;

    return AppRow(
      onTap: onTap,
      padding: const EdgeInsets.symmetric(vertical: Gap.md),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          // Знак выровнен по первой строке текста, а не по всему блоку:
          // на трёхстрочном названии он уехал бы в середину.
          LeadingGlyph(
            lineStyle: AppType.caption,
            child: Icon(
              group ? Icons.folder_outlined : Icons.description_outlined,
              size: 18,
              color: p.inkFaint,
            ),
          ),
          const SizedBox(width: Gap.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (showPath && unit.path.contains('/'))
                  Padding(
                    padding: const EdgeInsets.only(bottom: Gap.xs),
                    child: Text(
                      breadcrumb(unit.path),
                      style: AppType.caption.copyWith(color: p.inkFaint),
                    ),
                  ),
                // Метка набрана цифровым начертанием: в нём цифры одной
                // ширины, и столбец меток не пляшет от строки к строке.
                Text(
                  unit.label,
                  style: AppType.caseNumber.copyWith(color: p.accent),
                ),
                const SizedBox(height: Gap.xs),
                Prose(unit.title, style: AppType.body.copyWith(color: p.ink)),
                if (unit.statements > 0)
                  Padding(
                    padding: const EdgeInsets.only(top: Gap.xs),
                    child: Text(
                      counted(unit.statements, statementWord),
                      style: AppType.caption.copyWith(color: p.inkFaint),
                    ),
                  ),
              ],
            ),
          ),
          Icon(Icons.chevron_right, size: 18, color: p.inkFaint),
        ],
      ),
    );
  }
}

/// Путь без последнего звена: «где это лежит», а не «что это».
///
/// Последнее звено — сама единица, и повторять его над её же меткой
/// значит показывать «F32 › F32.1» и «F32.1» подряд.
String breadcrumb(String path) {
  final parts = path.split('/').where((one) => one.isNotEmpty).toList();
  if (parts.length < 2) return '';
  return parts.sublist(0, parts.length - 1).join(' › ');
}

/// Рубрика: её критерии и то, что внутри неё.
class UnitScreen extends StatefulWidget {
  const UnitScreen({
    super.key,
    required this.store,
    required this.source,
    required this.unit,
  });

  final ReferenceStore store;
  final RefSource source;
  final RefUnit unit;

  @override
  State<UnitScreen> createState() => _UnitScreenState();
}

class _UnitScreenState extends State<UnitScreen> {
  List<RefStatement> _statements = const [];
  List<RefUnit> _children = const [];
  Map<String, String> _links = const {};
  bool _loading = true;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final slug = widget.source.slug;
    final statements = await widget.store.statements(slug, widget.unit.label);
    final children = await widget.store.children(slug, widget.unit.label);
    // Обозначение, оказавшееся меткой того же источника, — это ссылка на
    // другую рубрику. Спрашивается справочник, а не вид метки: «похоже на
    // код МКБ» — тот самый дефект. Не нашлось — остаётся обозначение.
    final links = <String, String>{};
    for (final mark in {
      for (final one in statements)
        if (one.designation.isNotEmpty) one.designation,
    }) {
      final found = await widget.store.unit(slug, mark);
      if (found != null) links[mark] = found.title;
    }
    if (!mounted) return;
    setState(() {
      _statements = statements;
      _children = children;
      _links = links;
      _loading = false;
    });
  }

  /// Открывает рубрику, на которую сослалось положение.
  ///
  /// Полная рубрика читается из базы, а не собирается из метки и названия:
  /// экрану нужны и путь, и род, и число положений, а собранная наполовину
  /// рубрика показала бы «внутри: 0» у рубрики с вложенными.
  Future<void> _openLink(String label) async {
    final found = await widget.store.unit(widget.source.slug, label);
    if (!mounted || found == null) return;
    openUnit(context, widget.store, widget.source, found);
  }

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    final unit = widget.unit;
    final trail = breadcrumb(unit.path);

    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              // Метка в шапке, название — крупно ниже: врач пришёл сюда
              // за названием, а код он уже знает, иначе не нашёл бы
              // рубрику. Путь — подзаголовок шапки: он отвечает «где
              // это лежит», а это свойство места, а не рубрики.
              ScreenHeader(
                title: unit.label,
                subtitle: trail.isEmpty ? null : trail,
                onBack: () => Navigator.of(context).pop(),
              ),
              if (_loading)
                const Expanded(
                  child: Center(child: CircularProgressIndicator()),
                )
              else
                Expanded(
                  child: ListView(
                    padding: const EdgeInsets.only(bottom: Gap.xxl),
                    children: [
                      Prose(
                        unit.title,
                        style: AppType.titleL.copyWith(color: p.ink),
                      ),
                      const SizedBox(height: Gap.lg),
                      KindStrip(
                        statements: _statements,
                        children: _children.length,
                      ),
                      if (_statements.isNotEmpty) ...[
                        const SizedBox(height: Gap.xxl),
                        CriteriaList(
                          statements: _statements,
                          statementWord: widget.source.statementWord,
                          links: _links,
                          onLink: _openLink,
                        ),
                      ],
                      if (_children.isNotEmpty) ...[
                        const SizedBox(height: Gap.xxl),
                        const SectionLabel('Внутри'),
                        for (final child in _children)
                          UnitRow(
                            unit: child,
                            statementWord: widget.source.statementWord,
                            onTap: () => openUnit(
                              context,
                              widget.store,
                              widget.source,
                              child,
                            ),
                          ),
                      ],
                      if (_statements.isEmpty && _children.isEmpty)
                        EmptyState(
                          icon: const Icon(Icons.inbox_outlined),
                          title: 'Здесь пусто',
                          description:
                              'У этой '
                              '${widget.source.unitWord.toLowerCase()} '
                              'в источнике нет ни вложенного, ни отдельных '
                              '${pluralWord(widget.source.statementWord).toLowerCase()}',
                        ),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
