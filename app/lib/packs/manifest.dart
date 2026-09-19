/// Опись выпуска набора и сверка её подписи.
///
/// # Зачем устройству сверять подпись
///
/// Между устройством и сервером стоит чужая сеть, и подменить содержание
/// по дороге может всякий, кто в ней сидит. Заметить подмену можно только
/// подписью, и сверяет её именно устройство: сверка на сервере защищает
/// сервер от самого себя, а не врача от подменённого набора.
///
/// # Ключи приезжают в сборке, а не с набором
///
/// Выпуск несёт подпись и имя ключа, которым подписан, — но не сам ключ.
/// Приезжай ключ той же дверью, что и набор, подпись перестала бы значить
/// что-либо: подменивший набор подменил бы и ключ. Имя ключа нужно лишь
/// затем, чтобы выбрать верный из тех, что уже есть в сборке, — при смене
/// ключа прежние выпуски обязаны остаться проверяемыми прежним, а не стать
/// негодными все разом.
///
/// # Отпечаток у каждой задачи
///
/// Не один на весь набор: задачи качаются по одной, и проверка «весь набор
/// целиком» на обрыве посреди закачки не говорит ничего.
library;

import 'dart:convert';

import 'package:cryptography/cryptography.dart';

import 'canonical.dart';

/// Задача в описи выпуска: номер, порядок и отпечаток содержания.
class PackCase {
  const PackCase({required this.id, required this.ord, required this.hash});

  final String id;
  final int ord;
  final String hash;

  static PackCase? tryParse(Object? raw) {
    if (raw is! Map) return null;
    final id = raw['id'];
    final ord = raw['ord'];
    final hash = raw['hash'];
    if (id is! String || id.isEmpty) return null;
    if (ord is! int) return null;
    if (hash is! String || hash.isEmpty) return null;
    return PackCase(id: id, ord: ord, hash: hash);
  }
}

/// Выпуск набора: то, что подписано, и сама подпись.
class Release {
  const Release({
    required this.slug,
    required this.version,
    required this.title,
    required this.releasedAt,
    required this.signature,
    required this.keyId,
    required this.cases,
  });

  final String slug;
  final int version;
  final String title;

  /// Время выпуска строкой, как прислал сервер.
  ///
  /// Строкой, а не `DateTime`: подписаны байты, и разобрать время, чтобы
  /// собрать обратно, значит поручиться, что обратная сборка даст те же
  /// байты. Она их не даст — хотя бы потому, что `DateTime` в Dart пишет
  /// доли секунды, а сервер их не пишет. Показывать время можно разобрав
  /// эту строку, сверять — только ею самой.
  final String releasedAt;

  final String signature;
  final String keyId;
  final List<PackCase> cases;

  /// Разбирает ответ `GET /v1/packs/{slug}`.
  ///
  /// Непонятое не применяется: недостающее поле или негодная задача — это
  /// `null` целиком, а не выпуск с дырой. Поставить набор наполовину хуже,
  /// чем не поставить: врач увидит пропуски и решит, что задач просто нет.
  static Release? tryParse(Map<String, dynamic> raw) {
    final slug = raw['slug'];
    final version = raw['version'];
    final title = raw['title'];
    final releasedAt = raw['releasedAt'];
    final signature = raw['signature'];
    final keyId = raw['keyId'];
    final list = raw['cases'];
    if (slug is! String || slug.isEmpty) return null;
    if (version is! int) return null;
    if (title is! String) return null;
    if (releasedAt is! String || releasedAt.isEmpty) return null;
    if (signature is! String || signature.isEmpty) return null;
    if (keyId is! String || keyId.isEmpty) return null;
    if (list is! List) return null;

    final cases = <PackCase>[];
    for (final one in list) {
      final parsed = PackCase.tryParse(one);
      if (parsed == null) return null;
      cases.add(parsed);
    }
    return Release(
      slug: slug,
      version: version,
      title: title,
      releasedAt: releasedAt,
      signature: signature,
      keyId: keyId,
      cases: cases,
    );
  }

  /// Байты, которые подписаны.
  ///
  /// Задачи идут в порядке `ord`, а не в том, в каком их прислали: порядок
  /// в ответе задаёт сервер, и подпись, зависящая от него, сломалась бы от
  /// безобидной правки запроса. Правило записано в эталоне
  /// `shared/pack-manifest.json`, и сервер сворачивает манифест так же.
  String get canonical {
    final sorted = [...cases]..sort((a, b) => a.ord.compareTo(b.ord));
    return canonicalJson({
      'cases': [
        for (final one in sorted)
          {'hash': one.hash, 'id': one.id, 'ord': one.ord},
      ],
      'pack': slug,
      'releasedAt': releasedAt,
      'title': title,
      'version': version,
    });
  }
}

/// Открытые ключи, которым верит эта сборка.
///
/// Задаются при сборке (`--dart-define=CURATOR_PACK_KEYS=key-1:base64,…`),
/// а не литералом по месту и уж тем более не приезжают с сервера. Ключей
/// несколько намеренно: смена ключа не должна делать прежние выпуски
/// негодными все разом.
class TrustedKeys {
  const TrustedKeys(this._byId);

  final Map<String, String> _byId;

  /// Разбирает список вида `имя:база64,имя:база64`.
  ///
  /// Негодная запись отбрасывается, а не роняет разбор: одна опечатка в
  /// строке сборки не должна лишать приложение всех ключей сразу. Но и
  /// молчать нельзя — пустой набор ключей означает, что ни один набор не
  /// поставится, и об этом скажет сверка.
  factory TrustedKeys.parse(String raw) {
    final out = <String, String>{};
    for (final piece in raw.split(',')) {
      final at = piece.indexOf(':');
      if (at <= 0 || at == piece.length - 1) continue;
      out[piece.substring(0, at).trim()] = piece.substring(at + 1).trim();
    }
    return TrustedKeys(out);
  }

  bool get isEmpty => _byId.isEmpty;

  String? operator [](String keyId) => _byId[keyId];
}

/// Отказ сверки.
///
/// Ровно один текст на все случаи: и подменённое содержание, и чужой ключ,
/// и испорченная подпись означают одно — этому выпуску верить нельзя.
/// Разные тексты рассказали бы тому, кто подменяет, что именно у него не
/// сошлось.
class SignatureFailure implements Exception {
  const SignatureFailure([
    this.message =
        'Набор не прошёл проверку подлинности и не будет '
        'установлен. Попробуйте позже',
  ]);

  final String message;

  @override
  String toString() => message;
}

/// Сверяет подпись выпуска.
///
/// Отказывает, а не возвращает «не сошлось»: набор, про который известно,
/// что подпись не сошлась, дальше идти не должен ни по какой ветке, и
/// забытая проверка возвращённого значения — это установленный подменённый
/// набор.
Future<void> verifyRelease(Release release, TrustedKeys keys) async {
  final encoded = keys[release.keyId];
  if (encoded == null) {
    // Неизвестный ключ — это не «наверное, всё в порядке»: это либо
    // подмена, либо сборка старее выпуска. И то и другое значит, что
    // ставить набор нельзя.
    throw const SignatureFailure(
      'Этот набор подписан ключом, которого нет в приложении. '
      'Обновите приложение',
    );
  }
  final List<int> key;
  final List<int> signature;
  try {
    key = base64.decode(encoded);
    signature = base64.decode(release.signature);
  } on FormatException {
    throw const SignatureFailure();
  }
  if (key.length != 32 || signature.length != 64) {
    throw const SignatureFailure();
  }

  final ok = await Ed25519().verify(
    utf8.encode(release.canonical),
    signature: Signature(
      signature,
      publicKey: SimplePublicKey(key, type: KeyPairType.ed25519),
    ),
  );
  if (!ok) throw const SignatureFailure();
}

/// Считает отпечаток содержания задачи.
///
/// От канонического вида, а не от присланных байтов: сервер хранит
/// содержание в JSONB, который не хранит ни порядок ключей, ни пробелы, и
/// пересобирает объект по-своему. Отпечаток от присланного не сошёлся бы
/// ни у одной задачи.
Future<String> hashBody(Object? body) async {
  final sum = await Sha256().hash(utf8.encode(canonicalJson(body)));
  final hex = [
    for (final byte in sum.bytes) byte.toRadixString(16).padLeft(2, '0'),
  ].join();
  return 'sha256:$hex';
}
