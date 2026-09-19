/// Экран знаков и опыта.
///
/// Каталог показывается целиком, вместе с невыданными: знак, о котором
/// врач не знает, не мотивирует никого. Доля пути к невыданному видна и
/// без сети — её приложение считает само, и только её.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import 'metrics.dart';
import 'state.dart';

class ProgressScreen extends StatefulWidget {
  const ProgressScreen({super.key, required this.api});

  final Api api;

  @override
  State<ProgressScreen> createState() => _ProgressScreenState();
}

class _ProgressScreenState extends State<ProgressScreen> {
  Standing _standing = Standing.empty;
  List<SignStanding> _signs = [];
  bool _loading = true;
  ApiFailure? _failure;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _failure = null;
    });
    try {
      final standings = Standings(widget.api);
      final standing = await standings.standing();
      final signs = await standings.signs();
      if (!mounted) return;
      setState(() {
        _standing = standing;
        _signs = signs;
        _loading = false;
      });
    } on ApiFailure catch (failure) {
      if (!mounted) return;
      setState(() {
        _failure = failure;
        _loading = false;
      });
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Знаки')),
      body: SafeArea(child: _body(context)),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());

    final failure = _failure;
    if (failure != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(failure.message, textAlign: TextAlign.center),
              const SizedBox(height: 16),
              OutlinedButton(onPressed: _load, child: const Text('Ещё раз')),
            ],
          ),
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          StandingCard(standing: _standing),
          const SizedBox(height: 16),
          for (final sign in _signs) SignRow(sign: sign),
          if (_signs.isEmpty)
            const Text('Знаков пока нет', textAlign: TextAlign.center),
        ],
      ),
    );
  }
}

/// Опыт, уровень и величины.
class StandingCard extends StatelessWidget {
  const StandingCard({super.key, required this.standing});

  final Standing standing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        // Волосяная граница, а не тень.
        border: Border.all(color: theme.colorScheme.outlineVariant),
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Уровень ${standing.level}', style: theme.textTheme.titleLarge),
          Text('${standing.xp} опыта', style: theme.textTheme.bodyMedium),
          if (standing.due > 0) ...[
            const SizedBox(height: 8),
            Text('Ждут повторения: ${standing.due}'),
          ],
          const SizedBox(height: 12),
          // Величины показываются все, включая нулевые: у врача, ещё
          // ничего не решавшего, они нули, а не отсутствующие строки —
          // иначе не видно, к чему вообще можно идти.
          for (final metric in Metrics.catalog)
            Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Row(
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [
                  Text(metric.title, style: theme.textTheme.bodySmall),
                  Text(
                    '${standing.metrics[metric.key] ?? 0}',
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Знак строкой.
class SignRow extends StatelessWidget {
  const SignRow({super.key, required this.sign});

  final SignStanding sign;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 36,
            height: 36,
            child: CircularProgressIndicator(
              value: sign.progress,
              strokeWidth: 3,
              backgroundColor: theme.colorScheme.surfaceContainerHighest,
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(sign.title, style: theme.textTheme.titleSmall),
                Text(_words(), style: theme.textTheme.bodySmall),
              ],
            ),
          ),
        ],
      ),
    );
  }

  /// Что сказать про знак словами.
  ///
  /// Номер и тираж говорят врачу, каким он пришёл среди всех, и придумать
  /// их приложение не может — оно повторяет сказанное сервером.
  String _words() {
    if (sign.revoked) {
      // Отозванный знак остаётся в списке с отметкой, а не исчезает:
      // строка о выдаче не удаляется никогда.
      return 'Был ваш — перешёл другому';
    }
    if (sign.issued) {
      final serial = sign.serial > 0 ? '№ ${sign.serial}' : 'выдан';
      if (sign.editionSize > 0) {
        return '$serial из ${sign.editionSize}';
      }
      return serial;
    }
    if (sign.editionSize > 0) {
      final left = sign.editionSize - sign.issuedCount;
      // Тираж называется вслух: знак, который можно заслужить и не
      // получить, обязан сказать об этом до того, как он кончится.
      return 'Пройдено ${(sign.progress * 100).round()}%, '
          'осталось в тираже ${left < 0 ? 0 : left}';
    }
    return 'Пройдено ${(sign.progress * 100).round()}%';
  }
}
