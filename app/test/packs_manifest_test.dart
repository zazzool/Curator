/// Сверка приложения с общим эталоном подписи.
///
/// Здесь проверяется ровно то, ради чего эталон и заведён: приложение
/// обязано получить из манифеста ТЕ ЖЕ байты, что и сервер, и сверить по
/// ним подпись. Разойдись два способа — подпись не сойдётся ни на одном
/// устройстве, причём глазами оба текста будут выглядеть одинаково, и
/// причину будут искать неделю.
library;

import 'dart:convert';

import 'package:curator/packs/canonical.dart';
import 'package:curator/packs/manifest.dart';
import 'package:flutter_test/flutter_test.dart';

import 'shared_reference.dart';

/// Выпуск, собранный из примера в эталоне.
Release fromReference(Map<String, dynamic> reference) {
  final example = reference['example'] as Map<String, dynamic>;
  final manifest = example['manifest'] as Map<String, dynamic>;
  final release = Release.tryParse({
    'slug': manifest['pack'],
    'version': manifest['version'],
    'title': manifest['title'],
    'releasedAt': manifest['releasedAt'],
    'signature': example['signature'],
    'keyId': 'key-1',
    'cases': manifest['cases'],
  });
  if (release == null) {
    throw StateError('пример из эталона не разобрался: сверять нечего');
  }
  return release;
}

void main() {
  late Map<String, dynamic> reference;
  late TrustedKeys keys;

  setUp(() {
    reference = readShared('pack-manifest.json');
    final example = reference['example'] as Map<String, dynamic>;
    keys = TrustedKeys.parse('key-1:${example['publicKey']}');
  });

  test('канонический вид сходится с эталоном байт в байт', () {
    final example = reference['example'] as Map<String, dynamic>;
    expect(fromReference(reference).canonical, example['canonical']);
  });

  test('подпись из эталона сходится', () async {
    await expectLater(verifyRelease(fromReference(reference), keys), completes);
  });

  test('подменённый состав ломает подпись', () async {
    // Ради этого всё и написано: между устройством и сервером стоит чужая
    // сеть, и подменить состав по дороге может всякий, кто в ней сидит.
    final example = reference['example'] as Map<String, dynamic>;
    final manifest = example['manifest'] as Map<String, dynamic>;
    final cases = [
      for (final one in manifest['cases'] as List)
        {...one as Map<String, dynamic>},
    ];
    cases[0]['hash'] = 'sha256:ff';
    final swapped = Release.tryParse({
      'slug': manifest['pack'],
      'version': manifest['version'],
      'title': manifest['title'],
      'releasedAt': manifest['releasedAt'],
      'signature': example['signature'],
      'keyId': 'key-1',
      'cases': cases,
    })!;

    await expectLater(
      verifyRelease(swapped, keys),
      throwsA(isA<SignatureFailure>()),
    );
  });

  test('чужой ключ не подходит', () async {
    // Ключ настоящий по виду и годный по длине, но не наш.
    final other = TrustedKeys.parse(
      'key-1:${base64.encode(List<int>.filled(32, 7))}',
    );
    await expectLater(
      verifyRelease(fromReference(reference), other),
      throwsA(isA<SignatureFailure>()),
    );
  });

  test('незнакомое имя ключа отказывает, а не пропускает', () async {
    // «Ключа не знаю, значит наверное всё в порядке» — это установленный
    // подменённый набор.
    final other = TrustedKeys.parse('key-2:${keys['key-1']}');
    await expectLater(
      verifyRelease(fromReference(reference), other),
      throwsA(isA<SignatureFailure>()),
    );
  });

  test('испорченная подпись отказывает, а не падает', () async {
    final example = reference['example'] as Map<String, dynamic>;
    final manifest = example['manifest'] as Map<String, dynamic>;
    final broken = Release.tryParse({
      'slug': manifest['pack'],
      'version': manifest['version'],
      'title': manifest['title'],
      'releasedAt': manifest['releasedAt'],
      'signature': 'это вообще не база64',
      'keyId': 'key-1',
      'cases': manifest['cases'],
    })!;

    await expectLater(
      verifyRelease(broken, keys),
      throwsA(isA<SignatureFailure>()),
    );
  });

  test('выпуск без подписи не разбирается вовсе', () {
    // Непонятое не применяется: выпуск с дырой хуже, чем его отсутствие —
    // врач увидит пропуски и решит, что задач просто нет.
    final example = reference['example'] as Map<String, dynamic>;
    final manifest = example['manifest'] as Map<String, dynamic>;
    expect(
      Release.tryParse({
        'slug': manifest['pack'],
        'version': manifest['version'],
        'title': manifest['title'],
        'releasedAt': manifest['releasedAt'],
        'keyId': 'key-1',
        'cases': manifest['cases'],
      }),
      isNull,
    );
  });

  test('порядок задач в ответе на подпись не влияет', () {
    // Порядок в ответе задаёт сервер, и подпись, зависящая от него,
    // сломалась бы от безобидной правки запроса.
    final example = reference['example'] as Map<String, dynamic>;
    final manifest = example['manifest'] as Map<String, dynamic>;
    final backwards = (manifest['cases'] as List).reversed.toList();
    final shuffled = Release.tryParse({
      'slug': manifest['pack'],
      'version': manifest['version'],
      'title': manifest['title'],
      'releasedAt': manifest['releasedAt'],
      'signature': example['signature'],
      'keyId': 'key-1',
      'cases': backwards,
    })!;

    expect(shuffled.canonical, example['canonical']);
  });

  test('отпечаток содержания сходится с эталоном', () async {
    final body =
        (reference['bodyHash'] as Map<String, dynamic>)['example']
            as Map<String, dynamic>;
    final parsed = jsonDecode(body['body'] as String);
    expect(canonicalJson(parsed), body['canonical']);
    expect(await hashBody(parsed), body['hash']);
  });

  test('отпечаток не зависит от порядка ключей', () async {
    final first = await hashBody(jsonDecode('{"a":1,"b":{"x":1,"y":2}}'));
    final second = await hashBody(jsonDecode('{"b":{"y":2,"x":1},  "a":1}'));
    expect(first, second);
  });
}
