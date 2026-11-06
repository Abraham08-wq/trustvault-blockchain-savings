import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../format.dart';
import '../theme.dart';

/// One group: balance, ledger integrity, contribute, request money, your requests and activity.
class GroupScreen extends StatefulWidget {
  const GroupScreen({super.key, required this.api, required this.groupId});
  final Api api;
  final String groupId;

  @override
  State<GroupScreen> createState() => _GroupScreenState();
}

class _GroupScreenState extends State<GroupScreen> {
  Map<String, dynamic>? _group;
  Map<String, dynamic>? _verify;
  List<dynamic> _activity = const [];
  List<dynamic> _requests = const [];
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    final api = widget.api, id = widget.groupId;
    try {
      final results = await Future.wait([api.group(id), api.activity(id), api.withdrawals(id), api.verify(id)]);
      if (!mounted) return;
      final g = results[0] as Map<String, dynamic>;
      setState(() {
        _group = g;
        _activity = results[1] as List<dynamic>;
        _requests = (results[2] as List<dynamic>).where((w) => (w as Map<String, dynamic>)['requested_by'] == g['member_id']).toList();
        _verify = results[3] as Map<String, dynamic>;
        _error = null;
      });
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  String get _cur => (_group?['currency'] as String?) ?? 'KES';

  Future<void> _contribute() async {
    final amount = await showModalBottomSheet<int>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: Brand.deep,
      builder: (_) => _AmountSheet(
        title: 'Contribute through M-Pesa',
        help: 'You will get an M-Pesa prompt on this phone. Enter your PIN there to complete the payment.',
        currency: _cur,
        action: 'Send M-Pesa prompt',
      ),
    );
    if (amount == null || !mounted) return;
    try {
      final r = await widget.api.contribute(widget.groupId, amount);
      if (!mounted) return;
      showMessage(context, (r['message'] as String?) ?? 'Contribution started');
      await Future<void>.delayed(const Duration(milliseconds: 600));
      _load();
    } on ApiException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  Future<void> _requestWithdrawal() async {
    final req = await showModalBottomSheet<_WithdrawalInput>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      backgroundColor: Brand.deep,
      builder: (_) => _WithdrawalSheet(currency: _cur, available: (_group?['available'] as num?) ?? 0, myPhone: widget.api.phone),
    );
    if (req == null || !mounted) return;
    try {
      final w = await widget.api.requestWithdrawal(widget.groupId, req.amount, req.recipient, req.reason);
      if (!mounted) return;
      final level = (w['risk_level'] as String?) ?? 'low';
      final needed = (w['required_approvals'] as num?)?.toInt() ?? 0;
      showMessage(
        context,
        level == 'high'
            ? 'Request sent. Our fraud check flagged it, so it needs $needed approvals.'
            : 'Request sent. It needs $needed approvals from other members.',
      );
      _load();
    } on ApiException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  Future<void> _cancel(String id) async {
    try {
      await widget.api.cancel(id);
      if (mounted) showMessage(context, 'Request cancelled');
      _load();
    } on ApiException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    }
  }

  @override
  Widget build(BuildContext context) {
    final g = _group;
    return Scaffold(
      appBar: AppBar(title: Text((g?['group_name'] as String?) ?? 'Group')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
          children: [
            if (_error != null) Panel(accent: Brand.alert, child: Text(_error!)),
            if (g == null && _error == null) const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator())),
            if (g != null) ...[
              Text(money(g['balance'] as num, _cur), style: const TextStyle(fontSize: 38, fontWeight: FontWeight.w600)),
              const Text('Group balance', style: TextStyle(color: Brand.muted)),
              const SizedBox(height: 16),
              Row(children: [
                Expanded(child: _Figure(label: 'You have saved', value: money(g['my_contributions'] as num, _cur), color: Brand.safe)),
                const SizedBox(width: 12),
                Expanded(child: _Figure(label: 'Available to lend', value: money(g['available'] as num, _cur))),
              ]),
              const SizedBox(height: 16),
              _IntegrityCard(_verify),
              const SizedBox(height: 16),
              Row(children: [
                Expanded(child: FilledButton(onPressed: _contribute, child: const Text('Contribute'))),
                const SizedBox(width: 12),
                Expanded(child: OutlinedButton(onPressed: _requestWithdrawal, child: const Text('Request money'))),
              ]),
              const SizedBox(height: 8),
              Text(
                'Every withdrawal needs ${g['approval_threshold']} approvals from other members. Nobody can approve their own request.',
                style: const TextStyle(color: Brand.muted, fontSize: 13, height: 1.4),
              ),
              if (_requests.isNotEmpty) ...[
                const SizedBox(height: 28),
                const Text('Your requests', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                const SizedBox(height: 10),
                for (final w in _requests) ...[
                  _RequestTile(w as Map<String, dynamic>, currency: _cur, onCancel: () => _cancel(w['id'] as String)),
                  const SizedBox(height: 10),
                ],
              ],
              const SizedBox(height: 28),
              const Text('Group activity', style: TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
              const SizedBox(height: 4),
              const Text('Every record is linked to the one before it, so past entries cannot be changed quietly.',
                  style: TextStyle(color: Brand.muted, fontSize: 13)),
              const SizedBox(height: 10),
              if (_activity.isEmpty) const Panel(child: Text('No contributions yet. Be the first.')),
              for (final a in _activity) _ActivityRow(a as Map<String, dynamic>, currency: _cur),
            ],
          ],
        ),
      ),
    );
  }
}

class _Figure extends StatelessWidget {
  const _Figure({required this.label, required this.value, this.color = Brand.text});
  final String label, value;
  final Color color;

  @override
  Widget build(BuildContext context) => Panel(
        padding: const EdgeInsets.all(14),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          FittedBox(fit: BoxFit.scaleDown, child: Text(value, style: TextStyle(fontSize: 20, fontWeight: FontWeight.w600, color: color))),
          const SizedBox(height: 2),
          Text(label, style: const TextStyle(color: Brand.muted, fontSize: 12)),
        ]),
      );
}

/// Shows whether the group's ledger still matches the blockchain record.
class _IntegrityCard extends StatelessWidget {
  const _IntegrityCard(this.v);
  final Map<String, dynamic>? v;

  @override
  Widget build(BuildContext context) {
    IconData icon = Icons.hourglass_empty;
    Color color = Brand.muted;
    String title = 'Checking the records';
    String body = 'Comparing every record with the blockchain.';
    final r = v;
    if (r != null) {
      final valid = r['valid'] == true;
      final matches = r['anchor_matches'] == true && r['anchor_source'] == 'blockchain';
      final entries = (r['entries'] as num?)?.toInt() ?? 0;
      if (!valid) {
        icon = Icons.gpp_bad;
        color = Brand.alert;
        title = 'Records have been tampered with';
        body = r['broken_at_seq'] != null
            ? 'Record ${r['broken_at_seq']} was changed after it was saved. Tell your group officials immediately.'
            : 'The records no longer match the blockchain. Tell your group officials immediately.';
      } else if (matches || entries == 0) {
        icon = Icons.verified_user;
        color = Brand.safe;
        title = 'Records verified';
        body = entries == 0 ? 'No records yet.' : 'All $entries records match the copy sealed on the blockchain.';
      } else {
        icon = Icons.shield_outlined;
        color = Brand.caution;
        title = 'Waiting for the blockchain';
        body = 'The newest records will be sealed on the blockchain shortly.';
      }
    }
    return Panel(
      accent: color,
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(icon, color: color, size: 30),
        const SizedBox(width: 14),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: TextStyle(color: color, fontWeight: FontWeight.w700, fontSize: 15)),
            const SizedBox(height: 4),
            Text(body, style: const TextStyle(color: Brand.text, height: 1.35)),
          ]),
        ),
      ]),
    );
  }
}

class _RequestTile extends StatelessWidget {
  const _RequestTile(this.w, {required this.currency, required this.onCancel});
  final Map<String, dynamic> w;
  final String currency;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final status = w['status'] as String;
    final approvals = (w['approvals'] as List<dynamic>).length;
    final needed = (w['required_approvals'] as num).toInt();
    final label = switch (status) {
      'pending' => '$approvals of $needed approvals',
      'executed' => 'Paid out',
      'rejected' => 'Rejected by members',
      'cancelled' => 'Cancelled',
      _ => status,
    };
    final color = switch (status) {
      'executed' => Brand.safe,
      'rejected' => Brand.alert,
      'pending' => Brand.caution,
      _ => Brand.muted,
    };
    return Panel(
      accent: color,
      padding: const EdgeInsets.fromLTRB(16, 12, 8, 12),
      child: Row(children: [
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(money(w['amount'] as num, currency), style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
            const SizedBox(height: 2),
            Text('$label  ·  ${shortDate(w['created_at'] as String)}', style: TextStyle(color: color, fontSize: 13)),
          ]),
        ),
        if (status == 'pending') TextButton(onPressed: onCancel, child: const Text('Cancel')),
      ]),
    );
  }
}

class _ActivityRow extends StatelessWidget {
  const _ActivityRow(this.a, {required this.currency});
  final Map<String, dynamic> a;
  final String currency;

  @override
  Widget build(BuildContext context) {
    final amount = a['amount'] as num;
    final incoming = amount >= 0;
    final mine = a['mine'] == true;
    final who = mine ? 'You' : ((a['who'] as String?)?.isNotEmpty == true ? a['who'] as String : 'Member');
    final hash = (a['hash'] as String?) ?? '';
    return Container(
      padding: const EdgeInsets.symmetric(vertical: 12),
      decoration: const BoxDecoration(border: Border(bottom: BorderSide(color: Brand.rule))),
      child: Row(children: [
        CircleAvatar(
          radius: 18,
          backgroundColor: (incoming ? Brand.safe : Brand.caution).withValues(alpha: 0.14),
          child: Icon(incoming ? Icons.south_west : Icons.north_east, size: 18, color: incoming ? Brand.safe : Brand.caution),
        ),
        const SizedBox(width: 12),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(incoming ? '$who contributed' : 'Paid out for $who', style: const TextStyle(fontWeight: FontWeight.w500)),
            Text('#${a['seq']}  ${hash.length >= 10 ? hash.substring(0, 10) : hash}  ${shortDate(a['created_at'] as String)}',
                style: const TextStyle(color: Brand.muted, fontSize: 12, fontFamily: 'monospace')),
          ]),
        ),
        Text('${incoming ? '+' : '−'} ${money(amount.abs(), currency)}',
            style: TextStyle(fontWeight: FontWeight.w600, color: incoming ? Brand.safe : Brand.caution)),
      ]),
    );
  }
}

// ---------- bottom sheets ----------

class _AmountSheet extends StatefulWidget {
  const _AmountSheet({required this.title, required this.help, required this.currency, required this.action});
  final String title, help, currency, action;

  @override
  State<_AmountSheet> createState() => _AmountSheetState();
}

class _AmountSheetState extends State<_AmountSheet> {
  final _amount = TextEditingController();
  String? _error;

  void _submit() {
    final v = int.tryParse(_amount.text.trim());
    if (v == null || v <= 0) {
      setState(() => _error = 'Enter an amount greater than zero');
      return;
    }
    Navigator.pop(context, v);
  }

  @override
  void dispose() {
    _amount.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _SheetFrame(
        title: widget.title,
        help: widget.help,
        children: [
          TextField(
            controller: _amount,
            autofocus: true,
            keyboardType: TextInputType.number,
            inputFormatters: [FilteringTextInputFormatter.digitsOnly],
            style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600),
            decoration: field('Amount', prefix: '${widget.currency} ').copyWith(errorText: _error),
            onSubmitted: (_) => _submit(),
          ),
          const SizedBox(height: 16),
          FilledButton(onPressed: _submit, child: Text(widget.action)),
        ],
      );
}

class _WithdrawalInput {
  _WithdrawalInput(this.amount, this.recipient, this.reason);
  final int amount;
  final String recipient, reason;
}

class _WithdrawalSheet extends StatefulWidget {
  const _WithdrawalSheet({required this.currency, required this.available, required this.myPhone});
  final String currency;
  final num available;
  final String myPhone;

  @override
  State<_WithdrawalSheet> createState() => _WithdrawalSheetState();
}

class _WithdrawalSheetState extends State<_WithdrawalSheet> {
  final _amount = TextEditingController();
  final _reason = TextEditingController();
  late final _recipient = TextEditingController(text: widget.myPhone);
  String? _amountError, _phoneError;

  void _submit() {
    final v = int.tryParse(_amount.text.trim());
    setState(() {
      _amountError = (v == null || v <= 0)
          ? 'Enter an amount greater than zero'
          : (v > widget.available ? 'Only ${money(widget.available, widget.currency)} is available' : null);
      _phoneError = looksLikePhone(_recipient.text) ? null : 'Enter a valid phone number';
    });
    if (_amountError != null || _phoneError != null) return;
    Navigator.pop(context, _WithdrawalInput(v!, _recipient.text.trim(), _reason.text.trim()));
  }

  @override
  void dispose() {
    _amount.dispose();
    _reason.dispose();
    _recipient.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => _SheetFrame(
        title: 'Request money',
        help: 'Other members vote on your request. Money is sent only after enough of them approve.',
        children: [
          TextField(
            controller: _amount,
            autofocus: true,
            keyboardType: TextInputType.number,
            inputFormatters: [FilteringTextInputFormatter.digitsOnly],
            style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600),
            decoration: field('Amount', prefix: '${widget.currency} ', helper: 'Available: ${money(widget.available, widget.currency)}')
                .copyWith(errorText: _amountError),
          ),
          const SizedBox(height: 14),
          TextField(
            controller: _reason,
            maxLength: 300,
            decoration: field('What is it for?', hint: 'School fees, stock, medical bill'),
          ),
          const SizedBox(height: 6),
          TextField(
            controller: _recipient,
            keyboardType: TextInputType.phone,
            decoration: field('Send to M-Pesa number', helper: 'Your own number unless you change it').copyWith(errorText: _phoneError),
          ),
          const SizedBox(height: 16),
          FilledButton(onPressed: _submit, child: const Text('Send request')),
        ],
      );
}

class _SheetFrame extends StatelessWidget {
  const _SheetFrame({required this.title, required this.help, required this.children});
  final String title, help;
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => Padding(
        padding: EdgeInsets.fromLTRB(20, 0, 20, 20 + MediaQuery.of(context).viewInsets.bottom),
        child: SingleChildScrollView(
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text(title, style: const TextStyle(fontSize: 22, fontWeight: FontWeight.w600)),
            const SizedBox(height: 6),
            Text(help, style: const TextStyle(color: Brand.muted, height: 1.4)),
            const SizedBox(height: 20),
            ...children,
          ]),
        ),
      );
}
