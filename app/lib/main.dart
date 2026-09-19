/// Куратор: приложение обучающегося.
///
/// Сейчас здесь подъём и один экран, и больше ничего: остальное приезжает
/// по этапам сквозного пути. Пустое, но поднимающееся приложение лучше
/// заготовки на будущее — по нему сразу видно, что сборка собирается и
/// встаёт на устройство.
///
/// Расчёты, которые обязаны сходиться с сервером, уже здесь и сверены с
/// общими эталонами: опыт, уровни, повторение по интервалам, величины,
/// знаки и словарь событий. Они появились первыми не случайно — именно они
/// расходятся молча, и починить разошедшееся на руках у врача нечем.
library;

import 'package:flutter/material.dart';

void main() => runApp(const CuratorApp());

class CuratorApp extends StatelessWidget {
  const CuratorApp({super.key});

  @override
  Widget build(BuildContext context) {
    return MaterialApp(
      title: 'Куратор',
      theme: ThemeData(
        colorScheme: ColorScheme.fromSeed(seedColor: const Color(0xFF2F5D62)),
        // Плоскости разделяются волосяной границей, а не тенью: тень
        // допустима только у того, что физически висит над страницей.
        cardTheme: const CardThemeData(elevation: 0),
        appBarTheme: const AppBarTheme(elevation: 0, scrolledUnderElevation: 0),
      ),
      home: const StartScreen(),
    );
  }
}

class StartScreen extends StatelessWidget {
  const StartScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(title: const Text('Куратор')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text(
            'Приложение собирается. Задачи появятся здесь, '
            'когда доедут экраны разбора.',
            textAlign: TextAlign.center,
            style: theme.textTheme.bodyLarge,
          ),
        ),
      ),
    );
  }
}
