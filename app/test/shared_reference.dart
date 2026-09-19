/// Чтение общих эталонов.
///
/// Эталоны лежат вне приложения (`shared/`), потому что читают их обе
/// стороны. Отсутствие файла роняет проверку, а не пропускает её: сверка,
/// зовущая «пропустить» на недоступном эталоне, не сверяет ничего, а
/// выглядит зелёной.
library;

import 'dart:convert';
import 'dart:io';

Map<String, dynamic> readShared(String name) {
  final file = File('../shared/$name');
  if (!file.existsSync()) {
    throw StateError(
      'общий эталон ${file.path} не найден. Это отказ, а не пропуск: '
      'без него расчёт приложения и расчёт сервера расходятся молча',
    );
  }
  return jsonDecode(file.readAsStringSync()) as Map<String, dynamic>;
}
