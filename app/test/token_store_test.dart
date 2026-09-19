import 'package:curator/api/token_store.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shared_preferences/shared_preferences.dart';

/// Токен обязан пережить перезапуск.
///
/// Не переживи он — приложение заводило бы новую учётную запись при каждом
/// запуске, и врач терял бы прогресс, не понимая почему. Отдаётся токен
/// сервером ровно один раз, и напомнить его нельзя ничем.
void main() {
  setUp(() => SharedPreferences.setMockInitialValues({}));

  test('записанный токен читается заново', () async {
    await PrefsTokenStore().write('t-42');
    // Другой экземпляр — это и есть «после перезапуска»: памяти прежнего
    // у него нет.
    expect(await PrefsTokenStore().read(), 't-42');
  });

  test('пустого токена нет', () async {
    // Пустая строка, принятая за токен, оставила бы приложение навсегда
    // «заведённым» и не способным войти.
    SharedPreferences.setMockInitialValues({'device.token': ''});
    expect(await PrefsTokenStore().read(), isNull);
  });

  test('забытый токен не возвращается из памяти', () async {
    final store = PrefsTokenStore();
    await store.write('t-42');
    await store.clear();
    expect(await store.read(), isNull);
  });
}
