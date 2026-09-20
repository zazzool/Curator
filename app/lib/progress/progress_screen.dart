/// Экран знаков и опыта.
///
/// Каталог показывается целиком, вместе с невыданными: знак, о котором
/// врач не знает, не мотивирует никого. Доля пути к невыданному видна и
/// без сети — её приложение считает само, и только её.
///
/// Экран начинается тёмной зоной — приём донора, и он там не украшение:
/// это единственное место, где врач смотрит на себя, а не на задачу, и
/// смену основы глаз замечает раньше, чем читает слова. Почему зона
/// устроена так, а не иначе, — в `core/ui/surface.dart`.
library;

import 'package:flutter/material.dart';

import '../account/account.dart';
import '../account/account_screen.dart';
import '../api/client.dart';
import '../core/design/palette.dart';
import '../core/design/tokens.dart';
import '../core/design/typography.dart';
import '../core/ui/bars.dart';
import '../core/ui/motion.dart';
import '../core/ui/surface.dart';
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

  Future<void> _openAccount() async {
    // «Да» в ответе означает, что доступ вернули по почте и запись
    // сменилась. Перечитываем: числа на этом экране считаны за прежнюю
    // запись, и оставить их значило бы показать врачу чужой путь под его
    // вернувшейся почтой.
    final restored = await Navigator.of(context).push(
      MaterialPageRoute<bool>(
        builder: (_) => AccountScreen(account: Account(widget.api)),
      ),
    );
    if (restored == true) await _load();
  }

  @override
  Widget build(BuildContext context) => Scaffold(body: _body(context));

  Widget _body(BuildContext context) {
    if (_loading) return const Center(child: CircularProgressIndicator());

    final failure = _failure;
    if (failure != null) {
      return SafeArea(
        child: EmptyState(
          icon: const Icon(Icons.cloud_off_outlined),
          title: 'Знаки не загрузились',
          description: failure.message,
          action: OutlinedButton(
            onPressed: _load,
            child: const Text('Ещё раз'),
          ),
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        // Отступа сверху нет намеренно: тёмная зона заходит под системную
        // полосу состояния, и собственный отступ она считает сама.
        padding: EdgeInsets.zero,
        children: [
          StandingCard(
            standing: _standing,
            trailing: ScreenHeaderAction(
              icon: Icons.account_circle_outlined,
              // Доступ шестым разделом полосы не стал: мерка полосы —
              // «без него врач не может заниматься», и карточка ей не
              // отвечает. Но узнать, дошли ли деньги, было нельзя нигде,
              // и место для этого — там же, где врач смотрит на себя.
              label: 'Мой доступ',
              onTap: _openAccount,
            ),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(16, Gap.xl, 16, Gap.xxl),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                const SectionLabel('Знаки отличия'),
                if (_signs.isEmpty)
                  EmptyState(
                    icon: const Icon(Icons.military_tech_outlined),
                    title: 'Знаков пока нет',
                    description:
                        'Первый придёт за решённые задачи — считает их '
                        'сервер, и показать его раньше нечем.',
                  )
                else
                  // Каскадом, а не все разом: глаз успевает пройти список
                  // сверху вниз, а не получает готовую стену строк.
                  for (var i = 0; i < _signs.length; i++)
                    PopIn(
                      delay: PopIn.stagger(i),
                      child: SignRow(sign: _signs[i]),
                    ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Опыт, уровень и величины — тёмной зоной в начале экрана.
///
/// Зона входит в сам блок, а не оборачивает его снаружи: чернила здесь
/// свои ([AppPalette.heroInk]), и блок, отданный на светлую бумагу,
/// оказался бы нечитаемым ровно у того, кто его открыл.
class StandingCard extends StatelessWidget {
  const StandingCard({super.key, required this.standing, this.trailing});

  final Standing standing;

  /// Значок-действие в правом верхнем углу зоны. Цвет ему задаёт зона:
  /// приглушённые чернила на этой подложке не читаются.
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final p = context.palette;
    return HeroZone(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    // Заголовок объявлен заголовком для служб
                    // доступности: иначе экран читается как ровный поток
                    // текста, и понять, где он начинается, на слух нельзя.
                    Semantics(
                      header: true,
                      child: Text(
                        'Уровень ${standing.level}',
                        style: AppType.display.copyWith(color: p.heroInk),
                      ),
                    ),
                    Text(
                      '${standing.xp} опыта',
                      style: AppType.caption.copyWith(color: p.heroInkMuted),
                    ),
                  ],
                ),
              ),
              if (trailing != null) ...[
                const SizedBox(width: Gap.sm),
                IconTheme.merge(
                  data: IconThemeData(color: p.heroInk),
                  child: trailing!,
                ),
              ],
            ],
          ),
          if (standing.due > 0) ...[
            const SizedBox(height: Gap.lg),
            Pill(
              label: 'Ждут повторения: ${standing.due}',
              icon: const Icon(Icons.replay),
              color: p.heroInk,
              background: p.heroGlass,
            ),
          ],
          const SizedBox(height: Gap.xl),
          HeroTile(
            child: Column(
              children: [
                // Величины показываются все, включая нулевые: у врача,
                // ещё ничего не решавшего, они нули, а не отсутствующие
                // строки — иначе не видно, к чему вообще можно идти.
                for (final metric in Metrics.catalog)
                  Padding(
                    padding: const EdgeInsets.symmetric(vertical: 3),
                    child: Row(
                      children: [
                        Expanded(
                          child: Text(
                            metric.title,
                            style: AppType.caption.copyWith(
                              color: p.heroInkMuted,
                            ),
                          ),
                        ),
                        const SizedBox(width: Gap.md),
                        // Число набегает от нуля, а не появляется готовым:
                        // врач открывает этот экран посмотреть, сколько
                        // прибавилось, и движение — единственное, что
                        // отличает «115» от «было 115».
                        //
                        // Начертание цифровое: в нём цифры одной ширины,
                        // и колонка не дёргается по ходу счёта.
                        CountUp(
                          value: standing.metrics[metric.key] ?? 0,
                          style: AppType.numeral.copyWith(color: p.heroInk),
                        ),
                      ],
                    ),
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
    final p = context.palette;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: Gap.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  sign.title,
                  style: AppType.bodyStrong.copyWith(color: p.ink),
                ),
              ),
              const SizedBox(width: Gap.md),
              Text(
                '${(sign.progress * 100).round()}%',
                style: AppType.label.copyWith(color: p.inkMuted),
              ),
            ],
          ),
          const SizedBox(height: Gap.sm),
          // Тот же градиент, что и везде, где что-то заполняется: так
          // «доля пройденного» читается как одна величина, а не как пять
          // разных шкал на пяти экранах.
          ProgressBar(
            value: sign.progress,
            height: 8,
            gradient: p.energyGradient,
          ),
          const SizedBox(height: Gap.sm),
          Text(_words(), style: AppType.caption.copyWith(color: p.inkFaint)),
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
