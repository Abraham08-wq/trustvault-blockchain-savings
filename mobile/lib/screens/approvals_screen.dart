import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../theme.dart';

/// Withdrawals in the member's groups that are waiting for their vote.
class ApprovalsScreen extends StatefulWidget {
  const ApprovalsScreen({super.key, required this.api});
  final Api api;

  @override
  State<ApprovalsScreen> createState() => _ApprovalsScreenState();
}

class _ApprovalsScreenState extends State<ApprovalsScreen> {
  List<dynamic>? _items;
  String? _error;
  final Set<String> _busy = {};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final list = await widget.api.approvals();
      if (mounted) {
        setState(() {
          _items = list;
          _error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  Future<void> _vote(Map<String, dynamic> w, bool approve) async {
    final cur = w['currency'] as String;
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: Text(approve ? 'Approve this withdrawal?' : 'Reject this withdrawal?'),
        content: Text(approve
            ? '${money(w['amount'] as num, cur)} will be sent to ${w['recipient_phone']} once enough members approve.'
            : 'Your rejection is recorded. If enough members reject, the money stays in the group.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Go back')),
          TextButton(
            onPressed: () => Navigator.pop(c, true),
            child: Text(approve ? 'Approve' : 'Reject', style: TextStyle(color: approve ? Brand.safe : Brand.alert)),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final id = w['id'] as String;
    setState(() => _busy.add(id));
    try {
      final r = await widget.api.vote(id, approve);
      if (!mounted) return;
      final status = r['status'] as String?;
      showMessage(
        context,
        status == 'executed'
            ? 'Approved. Enough members agreed, so the money is being sent.'
            : status == 'rejected'
                ? 'Rejected. The withdrawal has been stopped.'
                : approve
                    ? 'Your approval was recorded.'
                    : 'Your rejection was recorded.',
      );
      await _load();
    } on ApiException catch (e) {
      if (mounted) showMessage(context, e.message, error: true);
    } finally {
      if (mounted) setState(() => _busy.remove(id));
    }
  }

  @override
  Widget build(BuildContext context) {
    final items = _items;
    return Scaffold(
      appBar: AppBar(title: const Text('Your votes')),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
          children: [
            if (_error != null) Panel(accent: Brand.alert, child: Text(_error!)),
            if (items == null && _error == null) const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator())),
            if (items != null && items.isEmpty)
              const Panel(
                child: Column(children: [
                  Icon(Icons.task_alt, color: Brand.safe, size: 40),
                  SizedBox(height: 10),
                  Text('Nothing waiting for your vote', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                  SizedBox(height: 4),
                  Text('When a member asks for money, it appears here for you to approve or reject.',
                      textAlign: TextAlign.center, style: TextStyle(color: Brand.muted)),
                ]),
              ),
            if (items != null)
              for (final w in items) ...[
                _VoteCard(
                  w as Map<String, dynamic>,
                  busy: _busy.contains(w['id']),
                  onApprove: () => _vote(w, true),
                  onReject: () => _vote(w, false),
                ),
                const SizedBox(height: 12),
              ],
          ],
        ),
      ),
    );
  }
}

class _VoteCard extends StatelessWidget {
  const _VoteCard(this.w, {required this.busy, required this.onApprove, required this.onReject});
  final Map<String, dynamic> w;
  final bool busy;
  final VoidCallback onApprove, onReject;

  @override
  Widget build(BuildContext context) {
    final cur = w['currency'] as String;
    final level = (w['risk_level'] as String?) ?? 'low';
    final color = Brand.risk(level);
    final reasons = ((w['risk_reasons'] as List<dynamic>?) ?? const []).cast<String>();
    final approvals = (w['approvals'] as List<dynamic>).length;
    final needed = (w['required_approvals'] as num).toInt();
    final reason = (w['reason'] as String?) ?? '';
    final levelText = switch (level) {
      'high' => 'High risk',
      'medium' => 'Medium risk',
      _ => 'Low risk',
    };
    return Panel(
      accent: color,
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Expanded(child: Text(w['group_name'] as String, style: const TextStyle(color: Brand.muted))),
          Tag(levelText, color),
        ]),
        const SizedBox(height: 10),
        Text(money(w['amount'] as num, cur), style: const TextStyle(fontSize: 28, fontWeight: FontWeight.w600)),
        const SizedBox(height: 4),
        Text(
          '${w['requester_name']} (${roleName((w['requester_role'] as String?) ?? 'member')}) asks to send it to ${w['recipient_phone']}',
          style: const TextStyle(height: 1.35),
        ),
        if (reason.isNotEmpty) ...[
          const SizedBox(height: 6),
          Text('Reason: $reason', style: const TextStyle(color: Brand.muted)),
        ],
        if (reasons.isNotEmpty) ...[
          const SizedBox(height: 12),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.all(12),
            decoration: BoxDecoration(color: color.withValues(alpha: 0.08), borderRadius: BorderRadius.circular(10)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Why our fraud check flagged it', style: TextStyle(color: color, fontWeight: FontWeight.w600, fontSize: 13)),
              const SizedBox(height: 6),
              for (final r in reasons)
                Padding(
                  padding: const EdgeInsets.only(top: 2),
                  child: Text('•  $r', style: const TextStyle(fontSize: 13, height: 1.35)),
                ),
            ]),
          ),
        ],
        const SizedBox(height: 12),
        Text('$approvals of $needed approvals so far', style: const TextStyle(color: Brand.muted, fontSize: 13)),
        const SizedBox(height: 12),
        Row(children: [
          Expanded(
            child: OutlinedButton(
              onPressed: busy ? null : onReject,
              style: OutlinedButton.styleFrom(foregroundColor: Brand.alert, side: const BorderSide(color: Brand.alert)),
              child: const Text('Reject'),
            ),
          ),
          const SizedBox(width: 12),
          Expanded(
            child: FilledButton(
              onPressed: busy ? null : onApprove,
              style: FilledButton.styleFrom(backgroundColor: Brand.safe, foregroundColor: const Color(0xFF02150E)),
              child: busy ? const SizedBox(width: 20, height: 20, child: CircularProgressIndicator(strokeWidth: 2)) : const Text('Approve'),
            ),
          ),
        ]),
      ]),
    );
  }
}
