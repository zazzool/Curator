/// Хранение токена устройства между запусками.
///
/// Токен отдаётся сервером ровно один раз — при заведении; дальше в базе
/// лежит только отпечаток, и «напомнить токен» невозможно by design.
/// Значит, потерянный токен — это потерянная учётная запись: приложение
/// заведёт новую, и врач лишится прогресса, не поняв почему.
library;

import 'package:shared_preferences/shared_preferences.dart';

import 'client.dart';

/// Токен в настройках устройства.
class PrefsTokenStore implements TokenStore {
  static const _key = 'device.token';

  // Прочитанное держится в памяти: к токену обращается каждый запрос, а
  // настройки — это работа с диском. Кэш согласован с записью здесь же и
  // другого писателя не имеет.
  String? _cached;

  @override
  Future<String?> read() async {
    if (_cached != null) return _cached;
    final prefs = await SharedPreferences.getInstance();
    final value = prefs.getString(_key);
    _cached = (value != null && value.isNotEmpty) ? value : null;
    return _cached;
  }

  @override
  Future<void> write(String token) async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_key, token);
    _cached = token;
  }

  @override
  Future<void> clear() async {
    final prefs = await SharedPreferences.getInstance();
    await prefs.remove(_key);
    _cached = null;
  }
}
