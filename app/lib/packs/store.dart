/// Наборы, лежащие на устройстве.
///
/// # Файлами, а не настройками
///
/// Набор на тысячу задач весит мегабайты, а настройки на Android читаются
/// в память целиком при запуске приложения: сложи мы набор туда, и запуск
/// стал бы тем дольше, чем больше врач скачал. Задачи лежат файлами, по
/// одному на задачу, — так же их и качают, по одной.
///
/// # Имя файла проверяется, а не берётся как есть
///
/// Номер задачи и имя набора приезжают с сервера, а сервер здесь — как раз
/// та сторона, которой мы не верим на слово: ради этого и заведена подпись.
/// Номер вида `../../secrets` записал бы файл мимо каталога приложения.
/// Поэтому в имени допущены только буквы латиницы, цифры, дефис, точка и
/// подчёркивание, и это проверяется, а не подразумевается.
library;

import 'dart:convert';
import 'dart:io';

import 'package:path_provider/path_provider.dart';

import 'manifest.dart';

/// Где лежат скачанные наборы.
abstract class PackStore {
  /// Кладёт опись выпуска. Опись кладётся ПОСЛЕ задач: пока её нет, набор
  /// считается недокачанным, и оборванная закачка не выдаёт себя за целую.
  Future<void> putRelease(Release release);

  Future<Release?> release(String slug);

  Future<void> putCase(String slug, String caseId, Object? body);

  Future<Object?> caseBody(String slug, String caseId);

  /// Номера задач, уже лежащих на устройстве, по возрастанию.
  Future<List<String>> have(String slug);

  Future<void> remove(String slug);

  /// Наборы, поставленные целиком, — то есть те, у которых есть опись.
  Future<List<String>> installed();
}

/// Имя, годное для файла.
///
/// Отказ, а не замена негодных знаков: заменив их, мы свели бы две разные
/// задачи к одному файлу, и вторая молча затёрла бы первую.
bool safeName(String name) {
  if (name.isEmpty || name.length > 128) return false;
  if (name == '.' || name == '..') return false;
  for (final unit in name.codeUnits) {
    final ok =
        (unit >= 0x61 && unit <= 0x7A) || // a-z
        (unit >= 0x41 && unit <= 0x5A) || // A-Z
        (unit >= 0x30 && unit <= 0x39) || // 0-9
        unit == 0x2D || // -
        unit == 0x5F || // _
        unit == 0x2E; // .
    if (!ok) return false;
  }
  return true;
}

/// Хранилище в каталоге приложения.
class FilePackStore implements PackStore {
  FilePackStore({Directory? root}) : _root = root;

  Directory? _root;

  Future<Directory> _dir() async {
    final root = _root ??= Directory(
      '${(await getApplicationDocumentsDirectory()).path}/packs',
    );
    if (!root.existsSync()) await root.create(recursive: true);
    return root;
  }

  Future<Directory> _pack(String slug) async {
    if (!safeName(slug)) {
      throw ArgumentError('негодное имя набора: $slug');
    }
    return Directory('${(await _dir()).path}/$slug');
  }

  @override
  Future<void> putRelease(Release release) async {
    final dir = await _pack(release.slug);
    if (!dir.existsSync()) await dir.create(recursive: true);
    await File('${dir.path}/release.json').writeAsString(
      jsonEncode({
        'slug': release.slug,
        'version': release.version,
        'title': release.title,
        'releasedAt': release.releasedAt,
        'signature': release.signature,
        'keyId': release.keyId,
        'cases': [
          for (final one in release.cases)
            {'id': one.id, 'ord': one.ord, 'hash': one.hash},
        ],
      }),
    );
  }

  @override
  Future<Release?> release(String slug) async {
    final file = File('${(await _pack(slug)).path}/release.json');
    if (!file.existsSync()) return null;
    try {
      final raw = jsonDecode(await file.readAsString());
      if (raw is! Map<String, dynamic>) return null;
      return Release.tryParse(raw);
    } on FormatException {
      // Испорченная опись отбрасывается целиком: набор без описи считается
      // недокачанным и будет скачан заново. Разбирать её по кускам значит
      // выдать половину за целое.
      return null;
    }
  }

  @override
  Future<void> putCase(String slug, String caseId, Object? body) async {
    if (!safeName(caseId)) {
      throw ArgumentError('негодный номер задачи: $caseId');
    }
    final dir = Directory('${(await _pack(slug)).path}/cases');
    if (!dir.existsSync()) await dir.create(recursive: true);
    await File('${dir.path}/$caseId.json').writeAsString(jsonEncode(body));
  }

  @override
  Future<Object?> caseBody(String slug, String caseId) async {
    if (!safeName(caseId)) return null;
    final file = File('${(await _pack(slug)).path}/cases/$caseId.json');
    if (!file.existsSync()) return null;
    try {
      return jsonDecode(await file.readAsString());
    } on FormatException {
      return null;
    }
  }

  @override
  Future<List<String>> have(String slug) async {
    final dir = Directory('${(await _pack(slug)).path}/cases');
    if (!dir.existsSync()) return [];
    final out = <String>[];
    for (final entry in dir.listSync()) {
      final name = entry.uri.pathSegments.last;
      if (name.endsWith('.json')) {
        out.add(name.substring(0, name.length - '.json'.length));
      }
    }
    out.sort();
    return out;
  }

  @override
  Future<void> remove(String slug) async {
    final dir = await _pack(slug);
    if (dir.existsSync()) await dir.delete(recursive: true);
  }

  @override
  Future<List<String>> installed() async {
    final root = await _dir();
    final out = <String>[];
    for (final entry in root.listSync()) {
      if (entry is! Directory) continue;
      final slug = entry.uri.pathSegments.where((p) => p.isNotEmpty).last;
      if (File('${entry.path}/release.json').existsSync()) out.add(slug);
    }
    out.sort();
    return out;
  }
}

/// Хранилище в памяти — для проверок.
class MemoryPackStore implements PackStore {
  final Map<String, Release> _releases = {};
  final Map<String, Map<String, Object?>> _cases = {};

  @override
  Future<void> putRelease(Release release) async =>
      _releases[release.slug] = release;

  @override
  Future<Release?> release(String slug) async => _releases[slug];

  @override
  Future<void> putCase(String slug, String caseId, Object? body) async {
    if (!safeName(caseId) || !safeName(slug)) {
      throw ArgumentError('негодное имя: $slug/$caseId');
    }
    (_cases[slug] ??= {})[caseId] = body;
  }

  @override
  Future<Object?> caseBody(String slug, String caseId) async =>
      _cases[slug]?[caseId];

  @override
  Future<List<String>> have(String slug) async =>
      (_cases[slug]?.keys.toList() ?? [])..sort();

  @override
  Future<void> remove(String slug) async {
    _releases.remove(slug);
    _cases.remove(slug);
  }

  @override
  Future<List<String>> installed() async => _releases.keys.toList()..sort();
}
