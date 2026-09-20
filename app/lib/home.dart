/// Разделы приложения.
///
/// Три: теория, практика, прогресс. Столько же и у донора, и это не
/// подражание ради подражания: мерка одна и та же — раздел заводится,
/// когда без него врач не может сделать то, ради чего приложение
/// поставлено. Теория — то, что он открывает у постели больного; практика
/// — то, ради чего ставил; прогресс — единственное место, где он смотрит
/// на себя.
///
/// Разделов было пять, и двое из них мерке не отвечали. «Повторение» —
/// это режим практики, а не место: врач приходит заниматься, а не
/// выбирать между двумя видами занятия, и теперь оно плитка на
/// «Практике», где видно, сколько задач ждёт сегодня. «Наборы» — витрина,
/// куда заходят раз в месяц: раздел полосы она занимала у занятия, а
/// дорог к ней теперь две, и обе там, где о наборах думают, — строкой на
/// «Практике» и строкой в «Настройках».
///
/// Оговорка «шестого раздела по этой мерке не будет» не стёрта, а
/// исправлена: мерка та же, счёт другой.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'core/design/palette.dart';
import 'cases/outbox.dart';
import 'cases/practice_hub_screen.dart';
import 'db/reference_store.dart';
import 'db/schedule.dart';
import 'packs/manifest.dart';
import 'packs/store.dart';
import 'progress/progress_screen.dart';
import 'reference/reference_screen.dart';

class Home extends StatefulWidget {
  const Home({
    super.key,
    required this.api,
    required this.outbox,
    required this.packs,
    required this.keys,
    required this.schedule,
    this.reference,
  });

  final Api api;
  final Outbox outbox;
  final PackStore packs;

  /// Открытые ключи, которым верит эта сборка. Приезжают сюда сверху, а не
  /// читаются по месту: ключ, взятый из двух мест, расходится молча.
  final TrustedKeys keys;

  /// Расписание повторения на устройстве. Пусто — значит, местная база не
  /// открылась, и повторения без сети не будет.
  final Schedule? schedule;

  /// Справочник на устройстве. Пусто по той же причине и с тем же
  /// следствием: раздел открывается и объясняет, почему пуст, а не
  /// исчезает из полосы. Исчезнувший раздел врач ищет как поломку.
  final ReferenceStore? reference;

  @override
  State<Home> createState() => _HomeState();
}

class _HomeState extends State<Home> {
  int _at = 0;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    // Область со службами заведена выше — над `MaterialApp`, а не здесь:
    // экраны, открытые поверх раздела, живут в навигаторе, а навигатор
    // стоит над нижним меню, и область отсюда им не видна вовсе. Поймала
    // это проверка, а не глаз: экран настроек падал на первом же кадре.
    return Scaffold(
      body: IndexedStack(
        index: _at,
        children: [
          ReferenceScreen(api: widget.api, store: widget.reference),
          const PracticeHubScreen(),
          ProgressScreen(api: widget.api),
        ],
      ),
      // Волосяная граница сверху, а не тень: полоса разделов лежит в
      // плоскости экрана, а не висит над ним. Тень здесь вернула бы
      // «пластиковый» вид, от которого всё оформление и уходит.
      bottomNavigationBar: DecoratedBox(
        decoration: BoxDecoration(
          border: Border(top: BorderSide(color: p.hairline)),
        ),
        child: NavigationBar(
          selectedIndex: _at,
          onDestinationSelected: (at) => setState(() => _at = at),
          // У каждого раздела один значок на оба состояния: выбранный
          // помечен подложкой и цветом, которые задаёт тема. Второй,
          // залитый значок — третий признак того же самого, и
          // разглядеть в нём смену начертания труднее, чем подложку
          // под пальцем.
          destinations: const [
            NavigationDestination(
              icon: Icon(Icons.menu_book_outlined),
              label: 'Теория',
            ),
            NavigationDestination(
              icon: Icon(Icons.school_outlined),
              label: 'Практика',
            ),
            NavigationDestination(
              icon: Icon(Icons.military_tech_outlined),
              label: 'Прогресс',
            ),
          ],
        ),
      ),
    );
  }
}
