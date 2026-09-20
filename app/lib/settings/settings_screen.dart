/// Экран «Настройки» и значок, который на него ведёт.
///
/// Экран стоит в стороне от нижнего меню намеренно — приём донора, и довод
/// у него не про красоту: настройки это «поставил и забыл», а разделы
/// меню — то, ради чего приложение открывают каждый день. Раздел меню,
/// отданный настройкам, отнимал бы место у занятия.
///
/// Дверь одна: значок в шапке каждого корневого экрана. Две двери в одно
/// место расходятся в поведении — у донора они успели разойтись: из шапки
/// экран открывался поверх занятия, из полосы — своей ветвью со своей
/// историей «назад».
///
/// **Опасного в корне нет.** Необратимое живёт там, где живёт его
/// предмет, — на подэкране, к которому относится: у одного действия не
/// должно быть двух разных дверей.
library;

import 'package:flutter/material.dart';

import '../account/account.dart';
import '../account/account_screen.dart';
import '../core/app_scope.dart';
import '../core/build_info.dart';
import '../core/design/tokens.dart';
import '../core/ui/surface.dart';
import '../packs/download.dart';
import '../packs/shelf.dart';
import '../packs/shelf_screen.dart';
import '../text/plural.dart';
import 'about_screen.dart';
import 'settings_kit.dart';

/// Значок настроек в шапке корневого экрана.
///
/// Свой виджет, а не `ScreenHeaderAction` по месту: значок, собранный
/// заново на каждом экране, разъезжается по подписи и по тому, куда
/// ведёт, — и разъезд этот врач видит раньше нас.
///
/// Живёт он здесь, а не в `core/ui/surface.dart` рядом с прочими частями
/// шапки: набору оформления неоткуда знать про экран настроек, и ссылка
/// оттуда сюда замкнула бы кольцо. У донора этой заботы нет — там значок
/// называет маршрут строкой.
class SettingsHeaderAction extends StatelessWidget {
  const SettingsHeaderAction({super.key, this.color});

  /// Цвет значка. Задаётся там, где шапка своего цвета: на тёмной зоне
  /// «Прогресса» обычные чернила не читаются.
  final Color? color;

  @override
  Widget build(BuildContext context) => ScreenHeaderAction(
    icon: Icons.settings_outlined,
    label: 'Настройки',
    color: color,
    // Поверх занятия, а не вместо него: возврат обязан вернуть врача
    // туда, откуда он смотрел.
    onTap: () => Navigator.of(
      context,
    ).push(MaterialPageRoute<void>(builder: (_) => const SettingsScreen())),
  );
}

class SettingsScreen extends StatefulWidget {
  const SettingsScreen({super.key});

  @override
  State<SettingsScreen> createState() => _SettingsScreenState();
}

class _SettingsScreenState extends State<SettingsScreen> {
  /// Сколько наборов лежит на устройстве. `null` — ещё не считали.
  int? _installed;

  /// Почта врача, как её назвал сервер. `null` — сервер ещё не ответил
  /// или не ответит вовсе.
  String? _email;

  @override
  void initState() {
    super.initState();
    // Обе строки состояния читаются молча и по отдельности: счёт наборов
    // лежит на устройстве и приходит сразу, почта едет с сервера и может
    // не доехать. Ни та, ни другая не задерживает экран — строки нет,
    // пока нечего сказать.
    WidgetsBinding.instance.addPostFrameCallback((_) => _read());
  }

  Future<void> _read() async {
    final scope = AppScope.of(context);
    try {
      final installed = await scope.packs.installed();
      if (!mounted) return;
      setState(() => _installed = installed.length);
    } catch (error) {
      // Счёт наборов читается с устройства, и отказ файлов уходил отсюда
      // необработанным: строки на экране не появлялось вовсе, а причина
      // не доезжала даже до журнала. Число остаётся непосчитанным, и
      // строки нет — «0 наборов» на непрочитанном врач принял бы за
      // правду.
      debugPrint('наборы на устройстве не сосчитались: $error');
    }
    try {
      final profile = await Account(scope.api).read();
      if (!mounted) return;
      setState(() => _email = profile.email);
    } catch (_) {
      // Молча: сказать врачу нечего — почта показывается в записи, а
      // экран настроек и без неё делает своё дело.
    }
  }

  Future<void> _openAccount() async {
    final scope = AppScope.of(context);
    final restored = await Navigator.of(context).push(
      MaterialPageRoute<bool>(
        builder: (_) => AccountScreen(account: Account(scope.api)),
      ),
    );
    if (!mounted) return;
    // «Да» значит, что доступ вернули по почте и запись сменилась. Числа
    // на разделах считаны за прежнюю запись, и оставить их значило бы
    // показать врачу чужой путь под его вернувшейся почтой. Объявляем это
    // один раз на всё приложение: разделов, которым надо перечитать,
    // больше одного, а возврат из настроек приходит только в тот, откуда
    // их открыли.
    if (restored == true) scope.accountEpoch.value++;
    await _read();
  }

  void _openPacks() {
    final scope = AppScope.of(context);
    Navigator.of(context).push(
      MaterialPageRoute<void>(
        builder: (_) => ShelfScreen(
          shelf: Shelf(scope.api, scope.packs),
          download: Download(scope.api, scope.packs, scope.keys),
          store: scope.packs,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final installed = _installed;
    final email = _email;

    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: Gap.screenH.add(const EdgeInsets.only(bottom: Gap.huge)),
          children: [
            ScreenHeader(
              title: 'Настройки',
              onBack: () => Navigator.of(context).pop(),
            ),
            // Порядок: сначала то, что врач меняет сам и часто, — наборы;
            // запись и сведения о сборке ниже.
            SettingsGroup(
              title: 'Наборы задач',
              rows: [
                SettingsNavRow(
                  title: 'Каталог наборов',
                  value: installed == null
                      ? null
                      : installed == 0
                      ? 'на устройстве пока ни одного'
                      : 'на устройстве '
                            '${withPlural(installed, 'набор', 'набора', 'наборов')}',
                  onTap: _openPacks,
                ),
              ],
            ),
            SettingsGroup(
              title: 'Учётная запись',
              rows: [
                SettingsNavRow(
                  title: 'Учётная запись',
                  // Пока сервер не ответил — строки нет: «почта не
                  // привязана» на непрочитанном означало бы не «нет», а
                  // «не знаем», и врач принял бы это за правду.
                  value: email == null
                      ? null
                      : email.isEmpty
                      ? 'почта не привязана'
                      : email,
                  onTap: _openAccount,
                ),
              ],
            ),
            SettingsGroup(
              title: 'Приложение',
              rows: [
                SettingsNavRow(
                  title: 'О приложении',
                  value: appVersion.isEmpty ? null : 'версия $appVersion',
                  onTap: () => Navigator.of(context).push(
                    MaterialPageRoute<void>(
                      builder: (_) => const AboutScreen(),
                    ),
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
