/// Мой доступ: кто я и что у меня есть.
///
/// Шестым разделом нижней полосы это не стало намеренно. Мерка полосы —
/// «без него врач не может сделать то, ради чего приложение поставлено», —
/// и доступ ей не отвечает: занимаются по задачам, а не по карточке. Но
/// узнать, дошли ли деньги и до какого числа есть подписка, было нельзя
/// нигде и никак: заплативший врач видел ровно то же, что не заплативший,
/// и спрашивал об этом нас.
library;

import 'package:flutter/material.dart';

import '../api/client.dart';
import 'account.dart';
import 'model.dart';

class AccountScreen extends StatefulWidget {
  const AccountScreen({super.key, required this.account});

  final Account account;

  @override
  State<AccountScreen> createState() => _AccountScreenState();
}

class _AccountScreenState extends State<AccountScreen> {
  Profile? _profile;
  ApiFailure? _failure;
  bool _loading = true;
  bool _saving = false;
  late final TextEditingController _name = TextEditingController();

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _failure = null;
    });
    try {
      final profile = await widget.account.read();
      if (!mounted) return;
      setState(() {
        _profile = profile;
        _loading = false;
        // Поле имени заполняется только при чтении, а не при каждой
        // перерисовке: иначе набранное врачом затиралось бы прежним
        // значением прямо под пальцем.
        _name.text = profile.displayName;
      });
    } on ApiFailure catch (failure) {
      if (!mounted) return;
      setState(() {
        _failure = failure;
        _loading = false;
      });
    }
  }

  Future<void> _rename() async {
    setState(() => _saving = true);
    try {
      await widget.account.rename(_name.text);
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(const SnackBar(content: Text('Имя сохранено')));
    } on ApiFailure catch (failure) {
      if (!mounted) return;
      ScaffoldMessenger.of(
        context,
      ).showSnackBar(SnackBar(content: Text(failure.message)));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Мой доступ')),
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

    final profile = _profile!;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.all(16),
        children: [
          _RightsCard(profile: profile),
          const SizedBox(height: 24),
          Text('Как вас звать', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Text(
            'Имя видно только вам и нам. В таблицах соперничества оно не '
            'показывается.',
            style: Theme.of(context).textTheme.bodySmall,
          ),
          const SizedBox(height: 8),
          TextField(
            controller: _name,
            decoration: const InputDecoration(border: OutlineInputBorder()),
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _rename(),
          ),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              onPressed: _saving ? null : _rename,
              child: const Text('Сохранить'),
            ),
          ),
          const SizedBox(height: 24),
          Text('Запись', style: Theme.of(context).textTheme.titleMedium),
          const SizedBox(height: 8),
          Text('Номер: ${profile.accountId}'),
          // Про почту сказано то, что есть. Обещать привязку «позже»
          // нельзя: привязки нет ни здесь, ни на сервере, и обещание
          // отправило бы врача ждать того, чего никто не делает.
          Text(
            profile.email.isEmpty
                ? 'Почта не привязана: запись живёт на этом устройстве.'
                : 'Почта: ${profile.email}',
          ),
        ],
      ),
    );
  }
}

class _RightsCard extends StatelessWidget {
  const _RightsCard({required this.profile});

  final Profile profile;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final rights = profile.rights;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Что у вас есть', style: theme.textTheme.titleMedium),
        const SizedBox(height: 8),
        if (rights.isEmpty)
          Text(
            'Платного доступа сейчас нет. Задачи из скачанных наборов '
            'остаются с вами — их не отбирают.',
            style: theme.textTheme.bodyMedium,
          )
        else
          for (final right in rights) _RightRow(right: right),
        if (profile.dropped > 0) ...[
          const SizedBox(height: 8),
          // Считается и показывается: молча выброшенное право выглядит
          // как «у вас его и не было», и объяснить это врачу нечем.
          // Число стоит ПОСЛЕ слова — по тому же доводу, что и в
          // lib/text/plural.dart: одной формы правила дают надёжно, а
          // трёх — нет.
          Text(
            'Прав, которых это приложение показать не умеет: '
            '${profile.dropped}. Обновите его.',
            style: theme.textTheme.bodySmall,
          ),
        ],
      ],
    );
  }
}

class _RightRow extends StatelessWidget {
  const _RightRow({required this.right});

  final Right right;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final until = right.until;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 6),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            right.kind == 'subscription'
                ? Icons.all_inclusive_outlined
                : Icons.inventory_2_outlined,
            size: 20,
            color: theme.colorScheme.primary,
          ),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  right.kind == 'subscription'
                      ? 'Подписка на весь корпус'
                      : 'Набор «${right.pack}»',
                  style: theme.textTheme.bodyLarge,
                ),
                Text(
                  right.forever
                      ? 'Бессрочно'
                      : until == null
                      ? 'Срок не разобрался'
                      : 'До ${asDay(until)}',
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

/// Дата словами: «19 октября 2026».
///
/// Имя латиницей не по вкусу, а по необходимости: кириллический
/// идентификатор в Dart не разбирается вовсе — «Illegal character».
/// По-русски здесь остаются комментарии и то, что читает врач.
String asDay(DateTime at) {
  const months = [
    'января',
    'февраля',
    'марта',
    'апреля',
    'мая',
    'июня',
    'июля',
    'августа',
    'сентября',
    'октября',
    'ноября',
    'декабря',
  ];
  final local = at.toLocal();
  return '${local.day} ${months[local.month - 1]} ${local.year}';
}
