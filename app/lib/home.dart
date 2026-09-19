/// Разделы приложения.
///
/// Четыре: задачи, повторение, наборы, знаки. Прежде их было три, и довод
/// был тот, что четвёртый раздел «на всякий случай» сделал бы нижнюю
/// полосу местом, где надо выбирать, вместо места, где всё видно сразу.
/// Довод остаётся в силе и здесь: наборы добавлены не «на всякий случай»,
/// а потому, что без них врач не может заниматься без сети — а заниматься
/// без сети и есть то, ради чего приложение поставлено. Пятого раздела по
/// этой мерке уже не будет.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'cases/outbox.dart';
import 'cases/practice_screen.dart';
import 'packs/manifest.dart';
import 'packs/download.dart';
import 'packs/shelf.dart';
import 'packs/shelf_screen.dart';
import 'packs/store.dart';
import 'progress/progress_screen.dart';

class Home extends StatefulWidget {
  const Home({
    super.key,
    required this.api,
    required this.outbox,
    required this.packs,
    required this.keys,
  });

  final Api api;
  final Outbox outbox;
  final PackStore packs;

  /// Открытые ключи, которым верит эта сборка. Приезжают сюда сверху, а не
  /// читаются по месту: ключ, взятый из двух мест, расходится молча.
  final TrustedKeys keys;

  @override
  State<Home> createState() => _HomeState();
}

class _HomeState extends State<Home> {
  int _at = 0;

  @override
  Widget build(BuildContext context) {
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
          ),
          PracticeScreen(
            api: widget.api,
            outbox: widget.outbox,
            packs: widget.packs,
            source: PracticeSource.review,
          ),
          ShelfScreen(
            shelf: Shelf(widget.api, widget.packs),
            download: Download(widget.api, widget.packs, widget.keys),
            store: widget.packs,
          ),
          ProgressScreen(api: widget.api),
        ],
      ),
      bottomNavigationBar: NavigationBar(
        selectedIndex: _at,
        onDestinationSelected: (at) => setState(() => _at = at),
        destinations: const [
          NavigationDestination(
            icon: Icon(Icons.school_outlined),
            selectedIcon: Icon(Icons.school),
            label: 'Задачи',
          ),
          NavigationDestination(
            icon: Icon(Icons.replay_outlined),
            selectedIcon: Icon(Icons.replay),
            label: 'Повторение',
          ),
          NavigationDestination(
            icon: Icon(Icons.inventory_2_outlined),
            selectedIcon: Icon(Icons.inventory_2),
            label: 'Наборы',
          ),
          NavigationDestination(
            icon: Icon(Icons.military_tech_outlined),
            selectedIcon: Icon(Icons.military_tech),
            label: 'Знаки',
          ),
        ],
      ),
    );
  }
}
