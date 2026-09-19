/// Разделы приложения.
///
/// Три и ровно три: задачи, повторение, знаки. Это и есть весь путь врача,
/// и четвёртый раздел «на всякий случай» сделал бы нижнюю полосу местом,
/// где надо выбирать, вместо места, где всё видно сразу.
library;

import 'package:flutter/material.dart';

import 'api/client.dart';
import 'cases/outbox.dart';
import 'cases/practice_screen.dart';
import 'progress/progress_screen.dart';

class Home extends StatefulWidget {
  const Home({super.key, required this.api, required this.outbox});

  final Api api;
  final Outbox outbox;

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
          PracticeScreen(api: widget.api, outbox: widget.outbox),
          PracticeScreen(
            api: widget.api,
            outbox: widget.outbox,
            source: PracticeSource.review,
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
            icon: Icon(Icons.military_tech_outlined),
            selectedIcon: Icon(Icons.military_tech),
            label: 'Знаки',
          ),
        ],
      ),
    );
  }
}
