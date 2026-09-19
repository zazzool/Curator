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
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(widget.source.title)),
      body: SafeArea(
        child: Column(
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 8, 16, 8),
              child: TextField(
                controller: _query,
                onChanged: _search,
                textInputAction: TextInputAction.search,
                decoration: InputDecoration(
                  isDense: true,
                  prefixIcon: const Icon(Icons.search, size: 20),
                  suffixIcon: _query.text.isEmpty
                      ? null
                      : IconButton(
                          icon: const Icon(Icons.close, size: 18),
                          onPressed: () {
                            _query.clear();
                            _search('');
                          },
                        ),
                  hintText:
                      'Код или название: ${widget.source.unitWord.toLowerCase()}',
                  border: const OutlineInputBorder(),
                ),
              ),
            ),
            if (_searching && _shown.isEmpty)
              Expanded(
                child: Center(
                  child: Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(
                      'Ничего не нашлось. Попробуйте часть кода '
                      'или одно слово из названия',
                      textAlign: TextAlign.center,
                      style: theme.textTheme.bodyMedium?.copyWith(
                        color: theme.colorScheme.onSurfaceVariant,
                      ),
                    ),
                  ),
                ),
              )
            else if (_loading)
              const Expanded(child: Center(child: CircularProgressIndicator()))
            else
              Expanded(
                child: ListView.builder(
                  padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
                  itemCount: _shown.length,
                  itemBuilder: (_, at) => UnitRow(
                    unit: _shown[at],
                    // В найденном путь показывается, а в дереве нет: в
                    // дереве врач и так знает, где он, а в выдаче поиска
                    // «F32.1» без «Расстройства настроения» над ним — это
                    // код без места.
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
    final theme = Theme.of(context);
    // Группа и запись отличаются знаком, и отличие взято у источника, а не
    // угадано по виду метки: «если это МКБ» — дефект.
    final group = unit.kind == 'group' || !unit.answerable;

    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(vertical: 10),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(
              group ? Icons.folder_outlined : Icons.description_outlined,
              size: 18,
              color: theme.colorScheme.outline,
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (showPath && unit.path.contains('/'))
                    Padding(
                      padding: const EdgeInsets.only(bottom: 2),
                      child: Text(
                        breadcrumb(unit.path),
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.outline,
                        ),
                      ),
                    ),
                  Text(
                    unit.label,
                    style: theme.textTheme.labelLarge?.copyWith(
                      color: theme.colorScheme.primary,
                    ),
                  ),
                  const SizedBox(height: 2),
                  Prose(unit.title, style: theme.textTheme.bodyMedium),
                  if (unit.statements > 0)
                    Padding(
                      padding: const EdgeInsets.only(top: 4),
                      child: Text(
                        counted(unit.statements, statementWord),
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                ],
              ),
            ),
            Icon(
              Icons.chevron_right,
              size: 18,
              color: theme.colorScheme.outlineVariant,
            ),
          ],
        ),
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
    final theme = Theme.of(context);
    final unit = widget.unit;
    final trail = breadcrumb(unit.path);

    return Scaffold(
      appBar: AppBar(title: Text(unit.label)),
      body: SafeArea(
        child: _loading
            ? const Center(child: CircularProgressIndicator())
            : ListView(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
                children: [
                  if (trail.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: 6),
                      child: Text(
                        trail,
                        style: theme.textTheme.labelSmall?.copyWith(
                          color: theme.colorScheme.outline,
                        ),
                      ),
                    ),
                  // Название рубрики — главное на экране, и оно набрано
                  // крупно: врач пришёл сюда за ним, а не за кодом,
                  // который уже знает.
                  Prose(unit.title, style: theme.textTheme.headlineSmall),
                  const SizedBox(height: 14),
                  KindStrip(
                    statements: _statements,
                    children: _children.length,
                  ),
                  if (_statements.isNotEmpty) ...[
                    const SizedBox(height: 22),
                    CriteriaList(
                      statements: _statements,
                      statementWord: widget.source.statementWord,
                      links: _links,
                      onLink: _openLink,
                    ),
                  ],
                  if (_children.isNotEmpty) ...[
                    const SizedBox(height: 26),
                    Text(
                      'Внутри',
                      style: theme.textTheme.titleSmall?.copyWith(
                        color: theme.colorScheme.primary,
                      ),
                    ),
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
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 32),
                      child: Text(
                        'У этой ${widget.source.unitWord.toLowerCase()} '
                        'в источнике нет ни вложенного, ни отдельных '
                        '${pluralWord(widget.source.statementWord).toLowerCase()}',
                        textAlign: TextAlign.center,
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: theme.colorScheme.onSurfaceVariant,
                        ),
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}
