import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Правила о самой сборке, которые не держит ни компилятор, ни тип.
void main() {
  test('версия уезжает вместе с заведением устройства', () {
    // Поле для версии в теле запроса было, параметр у ensureEnrolled был,
    // а вызывающий его не передавал — и на сервер приезжала пустота. То
    // есть про поле неизвестно, какие сборки на руках у врачей, при том
    // что вся доктрина проводной совместимости стоит именно на этом.
    //
    // Проверка читает ИСХОДНИК, а не поведение, и это не лень: заведение
    // случается один раз на устройство и до всякого экрана, а подменять
    // ради него дверь и хранилище значило бы проверять подделку. Ломается
    // она вместе с тем, что стережёт, — этого довольно.
    final source = File('lib/main.dart').readAsStringSync();
    final call = RegExp(
      r'ensureEnrolled\((?:[^)]|\([^)]*\))*\)',
      dotAll: true,
    ).firstMatch(source);
    expect(call, isNotNull, reason: 'заведение устройства не нашлось');
    expect(
      call!.group(0),
      contains('appVersion:'),
      reason: 'версия не уезжает на сервер: поле есть, а значения в нём нет',
    );
  });

  test('версия объявлена ровно в одном месте', () {
    // Две строки с версией разошлись бы молча, и разошлись бы ровно
    // тогда, когда версия важна.
    final declared = Directory('lib')
        .listSync(recursive: true)
        .whereType<File>()
        .where((f) => f.path.endsWith('.dart'))
        .where(
          (f) => f.readAsStringSync().contains(
            "String.fromEnvironment('CURATOR_VERSION')",
          ),
        )
        .map((f) => f.path)
        .toList();
    expect(declared, hasLength(1), reason: 'версия объявлена не единожды');
  });
}
