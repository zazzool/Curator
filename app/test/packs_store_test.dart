/// Проверки хранилища наборов на устройстве.
///
/// Каталог подставляется временным: настоящий даёт плагин, а плагина в
/// проверке нет. Проверяется при этом настоящая запись в настоящие файлы —
/// поддельное хранилище подделало бы вместе с записью и ловушку, ради
/// которой всё написано: имя файла приезжает с сервера.
library;

import 'dart:io';

import 'package:curator/packs/manifest.dart';
import 'package:curator/packs/store.dart';
import 'package:flutter_test/flutter_test.dart';

Release release(String slug, {int version = 1, List<Object?>? cases}) =>
    Release.tryParse({
      'slug': slug,
      'version': version,
      'title': 'Набор',
      'releasedAt': '2026-09-19T10:00:00Z',
      'signature': 'подпись',
      'keyId': 'key-1',
      'cases': cases ?? <Object?>[],
    })!;

void main() {
  late Directory root;
  late FilePackStore store;

  setUp(() {
    root = Directory.systemTemp.createTempSync('curator-packs');
    store = FilePackStore(root: root);
  });

  tearDown(() {
    if (root.existsSync()) root.deleteSync(recursive: true);
  });

  test('задача переживает перезапуск', () async {
    // Ради этого набор и ставится: заниматься без сети.
    await store.putCase('cardio', 'c-01', {'text': 'Условие'});
    final other = FilePackStore(root: root);
    expect(await other.caseBody('cardio', 'c-01'), {'text': 'Условие'});
  });

  test('набор без описи не считается поставленным', () async {
    // Опись ложится последней: она и есть отметка «набор стоит целиком».
    // Оборванная закачка не должна выдавать себя за целую.
    await store.putCase('cardio', 'c-01', {'text': 'Условие'});
    expect(await store.installed(), isEmpty);

    await store.putRelease(release('cardio'));
    expect(await store.installed(), ['cardio']);
  });

  test('имя с выходом из каталога отказывает, а не пишет мимо', () async {
    // Номер приезжает с сервера, а сервер здесь — как раз та сторона,
    // которой мы не верим на слово: ради этого и заведена подпись.
    await expectLater(
      store.putCase('cardio', '../../secrets', {}),
      throwsArgumentError,
    );
    await expectLater(
      store.putCase('../../etc', 'c-01', {}),
      throwsArgumentError,
    );
    // Мимо каталога ничего не легло.
    expect(
      root.parent.listSync().any((e) => e.path.endsWith('secrets.json')),
      isFalse,
    );
  });

  test('испорченная опись отбрасывается целиком', () async {
    // Разобрать её по кускам значит выдать половину за целое.
    await store.putRelease(release('cardio'));
    File('${root.path}/cardio/release.json').writeAsStringSync('не json');
    expect(await store.release('cardio'), isNull);
  });

  test('лежащие задачи отдаются по возрастанию номера', () async {
    // По возрастанию, потому что по нему же ходит курсор закачки.
    for (final id in ['c-03', 'c-01', 'c-02']) {
      await store.putCase('cardio', id, {'id': id});
    }
    expect(await store.have('cardio'), ['c-01', 'c-02', 'c-03']);
  });

  test('опись доезжает целой, вместе с отпечатками', () async {
    await store.putRelease(
      release(
        'cardio',
        version: 4,
        cases: [
          {'id': 'c-01', 'ord': 0, 'hash': 'sha256:aa'},
        ],
      ),
    );
    final back = await store.release('cardio');
    expect(back!.version, 4);
    expect(back.cases.single.hash, 'sha256:aa');
  });

  test('убранный набор исчезает целиком', () async {
    await store.putCase('cardio', 'c-01', {'text': 'Условие'});
    await store.putRelease(release('cardio'));

    await store.remove('cardio');

    expect(await store.installed(), isEmpty);
    expect(await store.have('cardio'), isEmpty);
    expect(await store.caseBody('cardio', 'c-01'), isNull);
  });

  test('чужие наборы при этом не трогаются', () async {
    await store.putRelease(release('cardio'));
    await store.putRelease(release('nevro'));

    await store.remove('cardio');

    expect(await store.installed(), ['nevro']);
  });
}
