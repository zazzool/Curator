/// Разделы приложения.
///
/// Пять: задачи, повторение, справочник, наборы, знаки. Прежде их было
/// три, и довод был тот, что раздел «на всякий случай» сделал бы нижнюю
/// полосу местом, где надо выбирать, вместо места, где всё видно сразу.
/// Мерка при этом одна и та же: раздел заводится, когда без него врач
/// не может сделать то, ради чего приложение поставлено.
///
/// Рядом с четвёртым разделом стояло «пятого по этой мерке уже не будет»,
/// и это оказалось неверно — не потому, что мерку смягчили, а потому, что
/// она не была применена к справочнику. Врач у постели больного открывает
/// не задачу, а критерии рубрики, и делает это без сети. Справочник,
/// доступный только изнутри разбора, в эту минуту не существует, а
/// спрятанный в наборы — не находится. Оговорка исправлена, а не стёрта:
/// шестой раздел потребует такого же довода, и «на всякий случай» им
/// по-прежнему не является.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'core/design/palette.dart';
import 'cases/outbox.dart';
import 'cases/practice_screen.dart';
import 'db/reference_store.dart';
import 'db/schedule.dart';
import 'packs/manifest.dart';
import 'packs/download.dart';
import 'packs/shelf.dart';
import 'packs/shelf_screen.dart';
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
    // Разделы держатся живыми, а не строятся заново: врач, отошедший на
    // знаки посреди задачи, вернулся бы к её началу — и ответ, уже
    // обдуманный, пришлось бы обдумывать снова.
    return Scaffold(
      body: IndexedStack(
        index: _at,
        children: [
          PracticeScreen(
            api: widget.api,
            outbox: widget.outbox,
            packs: widget.packs,
            schedule: widget.schedule,
          ),
          PracticeScreen(
            api: widget.api,
            outbox: widget.outbox,
            packs: widget.packs,
            schedule: widget.schedule,
            source: PracticeSource.review,
          ),
          ReferenceScreen(api: widget.api, store: widget.reference),
          ShelfScreen(
            shelf: Shelf(widget.api, widget.packs),
            download: Download(widget.api, widget.packs, widget.keys),
            store: widget.packs,
          ),
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
          // залитый значок — третий признак того же самого, и разглядеть
          // в нём смену начертания труднее, чем подложку под пальцем.
          destinations: const [
            NavigationDestination(
              icon: Icon(Icons.school_outlined),
              label: 'Задачи',
            ),
            NavigationDestination(
              icon: Icon(Icons.replay_outlined),
              label: 'Повторение',
            ),
            NavigationDestination(
              icon: Icon(Icons.menu_book_outlined),
              label: 'Справочник',
            ),
            NavigationDestination(
              icon: Icon(Icons.inventory_2_outlined),
              label: 'Наборы',
            ),
            NavigationDestination(
              icon: Icon(Icons.military_tech_outlined),
              label: 'Знаки',
            ),
          ],
        ),
      ),
    );
  }
}
