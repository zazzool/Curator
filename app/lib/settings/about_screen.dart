/// «О приложении»: назначение, дисклеймер, источники, благодарности.
///
/// Дорога сюда одна — строка в «Настройках», раздел «Приложение». Значка
/// «i» в шапке экранов нет намеренно: значок ничего не обещает, «О
/// приложении» называет себя словами, а место в шапке дороже сведений о
/// сборке.
library;

import 'package:flutter/material.dart';

import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/surface.dart';

/// Версия сборки, как её назвали при сборке.
///
/// Задаётся оттуда же, откуда адрес контура и ключи, — `--dart-define`.
/// Плагина, читающего версию из пакета, здесь нет и заводиться ради одной
/// строки он не будет: зависимость называется нуждой, а нужда тут не
/// набралась. Пусто — строки о версии нет вовсе: «версия —» сообщает
/// врачу ровно столько же, сколько её отсутствие, но занимает место и
/// выглядит поломкой.
const appVersion = String.fromEnvironment('CURATOR_VERSION');

class AboutScreen extends StatelessWidget {
  const AboutScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Scaffold(
      appBar: AppBar(title: const Text('О приложении')),
      body: ListView(
        padding: Gap.screenH.add(const EdgeInsets.only(bottom: Gap.huge)),
        children: [
          const SizedBox(height: Gap.sm),
          Center(
            child: Text(
              'Куратор',
              style: AppType.display.copyWith(color: p.ink),
              textAlign: TextAlign.center,
            ),
          ),
          const SizedBox(height: Gap.xs),
          Center(
            child: Text(
              'Учебные ситуационные задачи для врачей',
              style: AppType.body.copyWith(color: p.inkMuted),
              textAlign: TextAlign.center,
            ),
          ),
          const SizedBox(height: Gap.xl),
          if (appVersion.isNotEmpty) ...[
            _Block(title: 'Версия', body: appVersion),
            const SizedBox(height: Gap.md),
          ],
          const _Block(
            title: 'Медицинский дисклеймер',
            body:
                'Приложение — учебный справочник для врачей и не является '
                'средством постановки диагноза. Диагностические решения '
                'принимает врач на основании клинического обследования.',
          ),
          const SizedBox(height: Gap.md),
          const _Block(
            title: 'Источники критериев',
            body:
                'Тексты критериев — производные материалы от изданий ВОЗ: '
                '«Clinical descriptions and diagnostic guidelines» (ВОЗ, '
                '1992) и «Diagnostic criteria for research» (ВОЗ, 1993), а '
                'также клинических рекомендаций Минздрава России. '
                'Библиографическая ссылка и дата сверки указаны у каждого '
                'источника в разделе «Теория».',
          ),
          const SizedBox(height: Gap.md),
          const _Block(
            title: 'Шрифт',
            body:
                'Golos Text — шрифт с открытой лицензией SIL OFL 1.1, '
                'спроектированный для кириллицы. Благодарим Paratype и '
                'проект Golos Text.',
          ),
        ],
      ),
    );
  }
}

class _Block extends StatelessWidget {
  const _Block({required this.title, required this.body});

  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return Surface(
      padding: const EdgeInsets.all(Gap.xl),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: AppType.titleS.copyWith(color: p.ink)),
          const SizedBox(height: Gap.md),
          Text(body, style: AppType.body.copyWith(color: p.inkMuted)),
        ],
      ),
    );
  }
}
