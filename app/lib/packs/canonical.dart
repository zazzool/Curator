/// Приведение к подписываемым байтам.
///
/// # Зачем свой сборщик, а не `jsonEncode`
///
/// Подпись сходится лишь тогда, когда обе стороны сворачивают одно и то же
/// в ОДНИ И ТЕ ЖЕ байты. Сервер написан на Go, приложение на Dart, и их
/// стандартные сборщики JSON расходятся в трёх местах — молча, без единого
/// отказа, и глазами оба текста выглядят одинаково:
///
/// 1. **Порядок ключей.** `jsonEncode` пишет карту в порядке вставки, Go —
///    по алфавиту. Правило эталона: по алфавиту на всех уровнях.
/// 2. **Целое, записанное дробью.** `jsonDecode("1.0")` даёт `double`, и
///    Dart пишет его обратно как `1.0`; Go на том же месте пишет `1`.
///    Число, пришедшее из ответа сервера дробью без остатка, — обычное
///    дело, и отпечаток задачи из-за него не сошёлся бы.
/// 3. **Экранирование.** Go экранирует U+2028 и U+2029 всегда, даже с
///    выключенным экранированием HTML, — Dart оставляет их как есть.
///    Символы редкие, но расхождение на них неотличимо от подмены.
///
/// Прочее экранирование у обеих сторон совпадает, и это **замерено**, а не
/// принято на веру: догадка «Go пишет управляющие только длинной записью»
/// уже была здесь и оказалась неверной. `\b`, `\f`, `\n`, `\r`, `\t`
/// пишутся коротко, прочие управляющие — как `\u00xx` строчными. Замер
/// сделан на go1.25, и держат его встречные проверки с обеих сторон
/// (`server/internal/packs/manifest_test.go` и
/// `app/test/packs_canonical_test.dart`): поменяй Go своё поведение при
/// обновлении — упадут они, а не подпись на устройстве врача.
///
/// Поэтому байты собираются здесь по правилу эталона
/// `shared/pack-manifest.json`, а не отдаются библиотеке.
library;

/// Сворачивает значение в канонический вид: ключи по алфавиту, без
/// пробелов, UTF-8 как есть, `<`, `>` и `&` не экранируются.
///
/// Негодное значение (то, чего в JSON не бывает) — отказ, а не пропуск:
/// подписать то, чего мы не поняли, значит поручиться за это.
String canonicalJson(Object? value) {
  final out = StringBuffer();
  _write(out, value);
  return out.toString();
}

void _write(StringBuffer out, Object? value) {
  if (value == null) {
    out.write('null');
    return;
  }
  if (value is bool) {
    out.write(value ? 'true' : 'false');
    return;
  }
  if (value is num) {
    _number(out, value);
    return;
  }
  if (value is String) {
    _string(out, value);
    return;
  }
  if (value is List) {
    out.write('[');
    for (var i = 0; i < value.length; i++) {
      if (i > 0) out.write(',');
      _write(out, value[i]);
    }
    out.write(']');
    return;
  }
  if (value is Map) {
    // Ключи по алфавиту — по кодовым единицам строки, как сравнивает их
    // Go. Сортировка «по-человечески», с учётом языка, дала бы другой
    // порядок у кириллицы, и подпись не сошлась бы только у русских имён.
    final keys = value.keys.map((k) {
      if (k is! String) {
        throw FormatException('ключ объекта не строка: $k');
      }
      return k;
    }).toList()..sort();
    out.write('{');
    for (var i = 0; i < keys.length; i++) {
      if (i > 0) out.write(',');
      _string(out, keys[i]);
      out.write(':');
      _write(out, value[keys[i]]);
    }
    out.write('}');
    return;
  }
  throw FormatException('в канонический вид не сворачивается: $value');
}

void _number(StringBuffer out, num value) {
  if (value is int) {
    out.write(value.toString());
    return;
  }
  final asDouble = value as double;
  if (asDouble.isNaN || asDouble.isInfinite) {
    // В JSON таких чисел нет, и придумывать им запись нельзя: любая
    // выдумка разойдётся с чужой выдумкой.
    throw FormatException('число не записывается в JSON: $asDouble');
  }
  // Целое, приехавшее дробью, пишется целым: Go пишет float64(1) как `1`,
  // и `1.0` с нашей стороны сломало бы отпечаток на ровном месте.
  if (asDouble == asDouble.roundToDouble() && asDouble.abs() < 1e21) {
    out.write(asDouble.toInt().toString());
    return;
  }
  out.write(asDouble.toString());
}

void _string(StringBuffer out, String value) {
  out.write('"');
  for (final unit in value.codeUnits) {
    switch (unit) {
      case 0x22:
        out.write(r'\"');
      case 0x5C:
        out.write(r'\\');
      case 0x0A:
        out.write(r'\n');
      case 0x0D:
        out.write(r'\r');
      case 0x09:
        out.write(r'\t');
      case 0x08:
        out.write(r'\b');
      case 0x0C:
        out.write(r'\f');
      // U+2028 и U+2029 Go экранирует всегда — даже с выключенным
      // экранированием HTML. Это наследство JavaScript, где они рвут
      // строку, и отменить его в Go нечем; значит, повторяем.
      case 0x2028:
        out.write(r'\u2028');
      case 0x2029:
        out.write(r'\u2029');
      default:
        if (unit < 0x20) {
          // Прочие управляющие — длинной записью, строчными буквами: так
          // их пишет и Go.
          out.write('\\u${unit.toRadixString(16).padLeft(4, '0')}');
        } else {
          out.writeCharCode(unit);
        }
    }
  }
  out.write('"');
}
