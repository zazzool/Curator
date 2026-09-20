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

import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/leading_glyph.dart';
import '../core/ui/surface.dart';
import '../api/client.dart';
import 'account.dart';
import 'email_card.dart';
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

  /// Доступ вернулся: токен сменился, и запись теперь другая.
  ///
  /// Закрываем экран с ответом «да»: перечитать надо не только его —
  /// лента, повторение и знаки показывают прежнюю запись, и оставь мы их
  /// как есть, врач увидел бы чужие числа под своей вернувшейся почтой.
  void _recovered() {
    if (!mounted) return;
    Navigator.of(context).pop(true);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Padding(
          padding: Gap.screenH,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              ScreenHeader(
                // «Учётная запись», а не «Мой доступ»: подэкран
                // настроек называется тем же словом, что и строка,
                // которая на него ведёт, — иначе врач не уверен, что
                // попал туда, куда шёл. Доступ, почта и имя живут здесь
                // вместе: это всё про одну запись.
                title: 'Учётная запись',
                onBack: () => Navigator.of(context).pop(),
              ),
              Expanded(child: _body(context)),
            ],
          ),
        ),
      ),
    );
  }

  Widget _body(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());

    final failure = _failure;
    if (failure != null) {
      return EmptyState(
        icon: const Icon(Icons.cloud_off_outlined),
        title: 'Запись не загрузилась',
        description: failure.message,
        action: OutlinedButton(onPressed: _load, child: const Text('Ещё раз')),
      );
    }

    final profile = _profile!;
    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.only(bottom: Gap.xxl),
        children: [
          _RightsCard(profile: profile),
          const SizedBox(height: Gap.xxl),
          const SectionLabel('Как вас звать'),
          Text(
            'Имя видно только вам и нам. В таблицах соперничества оно не '
            'показывается.',
            style: AppType.caption.copyWith(color: context.palette.inkFaint),
          ),
          const SizedBox(height: Gap.sm),
          TextField(
            // Метка проверке: полей на экране теперь несколько, и «первое
            // попавшееся» однажды окажется полем почты — молча, потому
            // что вводится туда тоже текст.
            key: const Key('поле имени'),
            controller: _name,
            textInputAction: TextInputAction.done,
            onSubmitted: (_) => _rename(),
          ),
          const SizedBox(height: Gap.sm),
          Align(
            alignment: Alignment.centerRight,
            child: FilledButton(
              onPressed: _saving ? null : _rename,
              child: const Text('Сохранить'),
            ),
          ),
          const SizedBox(height: Gap.xxl),
          EmailCard(
            account: widget.account,
            email: profile.email,
            // Перечитываем, а не дописываем привязанный адрес в свою
            // копию: правда о записи лежит на сервере, и вторая копия
            // расходится с ней молча.
            onBound: (_) => _load(),
            onRecovered: _recovered,
          ),
          const SizedBox(height: Gap.xxl),
          const SectionLabel('Запись'),
          Text(
            'Номер: ${profile.accountId}',
            style: AppType.body.copyWith(color: context.palette.inkMuted),
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
    final p = context.palette;
    final rights = profile.rights;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        const SectionLabel('Что у вас есть'),
        if (rights.isEmpty)
          Text(
            'Платного доступа сейчас нет. Задачи из скачанных наборов '
            'остаются с вами — их не отбирают.',
            style: AppType.body.copyWith(color: p.inkMuted),
          )
        else
          for (final right in rights) _RightRow(right: right),
        if (profile.dropped > 0) ...[
          const SizedBox(height: Gap.sm),
          // Считается и показывается: молча выброшенное право выглядит
          // как «у вас его и не было», и объяснить это врачу нечем.
          // Число стоит ПОСЛЕ слова — по тому же доводу, что и в
          // lib/text/plural.dart: одной формы правила дают надёжно, а
          // трёх — нет.
          Text(
            'Прав, которых это приложение показать не умеет: '
            '${profile.dropped}. Обновите его.',
            style: AppType.caption.copyWith(color: p.inkFaint),
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
    final p = context.palette;
    final until = right.until;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: Gap.sm),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          LeadingGlyph(
            lineStyle: AppType.body,
            child: Icon(
              right.kind == 'subscription'
                  ? Icons.all_inclusive_outlined
                  : Icons.inventory_2_outlined,
              size: 20,
              color: p.accent,
            ),
          ),
          const SizedBox(width: Gap.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  right.kind == 'subscription'
                      ? 'Подписка на весь корпус'
                      : 'Набор «${right.pack}»',
                  style: AppType.bodyStrong.copyWith(color: p.ink),
                ),
                Text(
                  right.forever
                      ? 'Бессрочно'
                      : until == null
                      ? 'Срок не разобрался'
                      : 'До ${asDay(until)}',
                  style: AppType.caption.copyWith(color: p.inkFaint),
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
