import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../theme.dart';
import 'approvals_screen.dart';
import 'group_screen.dart';

/// The member's groups, with a prompt when votes are waiting.
class HomeScreen extends StatefulWidget {
  const HomeScreen({super.key, required this.api, required this.onSignedOut});
  final Api api;
  final VoidCallback onSignedOut;

  @override
  State<HomeScreen> createState() => _HomeScreenState();
}

class _HomeScreenState extends State<HomeScreen> {
  Map<String, dynamic>? _data;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final d = await widget.api.home();
      if (mounted) {
        setState(() {
          _data = d;
          _error = null;
        });
      }
    } on ApiException catch (e) {
      if (mounted) setState(() => _error = e.message);
    }
  }

  Future<void> _signOut() async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (c) => AlertDialog(
        title: const Text('Sign out?'),
        content: const Text('You will need a new SMS code to sign in again.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(c, false), child: const Text('Stay signed in')),
          TextButton(onPressed: () => Navigator.pop(c, true), child: const Text('Sign out')),
        ],
      ),
    );
    if (ok != true) return;
    await widget.api.signOut();
    widget.onSignedOut();
  }

  Future<void> _open(Widget screen) async {
    await Navigator.of(context).push(MaterialPageRoute<void>(builder: (_) => screen));
    _load();
  }

  @override
  Widget build(BuildContext context) {
    final d = _data;
    final groups = (d?['groups'] as List<dynamic>?) ?? const [];
    final pending = (d?['pending_approvals'] as num?)?.toInt() ?? 0;
    final name = (d?['name'] as String?) ?? '';
    return Scaffold(
      appBar: AppBar(
        title: const Text('ChamaChain'),
        actions: [
          IconButton(
            tooltip: 'Withdrawals waiting for your vote',
            onPressed: () => _open(ApprovalsScreen(api: widget.api)),
            icon: Badge(isLabelVisible: pending > 0, label: Text('$pending'), child: const Icon(Icons.how_to_vote_outlined)),
          ),
          PopupMenuButton<String>(
            onSelected: (v) {
              if (v == 'out') _signOut();
            },
            itemBuilder: (_) => const [PopupMenuItem(value: 'out', child: Text('Sign out'))],
          ),
        ],
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(
          padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
          children: [
            if (_error != null) Padding(padding: const EdgeInsets.only(bottom: 12), child: _ErrorBox(_error!, onRetry: _load)),
            if (d == null && _error == null) const Padding(padding: EdgeInsets.all(48), child: Center(child: CircularProgressIndicator())),
            if (d != null) ...[
              Text(name.isEmpty ? 'Welcome' : 'Hello, ${name.split(' ').first}',
                  style: const TextStyle(fontSize: 26, fontWeight: FontWeight.w300)),
              const SizedBox(height: 4),
              Text(widget.api.phone, style: const TextStyle(color: Brand.muted)),
              const SizedBox(height: 20),
              if (pending > 0) ...[
                Panel(
                  accent: Brand.caution,
                  onTap: () => _open(ApprovalsScreen(api: widget.api)),
                  child: Row(children: [
                    const Icon(Icons.how_to_vote, color: Brand.caution),
                    const SizedBox(width: 14),
                    Expanded(
                      child: Text(
                        pending == 1 ? '1 withdrawal is waiting for your vote' : '$pending withdrawals are waiting for your vote',
                        style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600),
                      ),
                    ),
                    const Icon(Icons.chevron_right, color: Brand.muted),
                  ]),
                ),
                const SizedBox(height: 16),
              ],
              if (groups.isEmpty)
                const Panel(child: Text('You are not an active member of any group yet. Ask your group official to add this phone number.')),
              for (final g in groups) ...[
                _GroupCard(g as Map<String, dynamic>, onTap: () => _open(GroupScreen(api: widget.api, groupId: g['group_id'] as String))),
                const SizedBox(height: 12),
              ],
            ],
          ],
        ),
      ),
    );
  }
}

class _GroupCard extends StatelessWidget {
  const _GroupCard(this.g, {required this.onTap});
  final Map<String, dynamic> g;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final cur = g['currency'] as String;
    final pending = (g['pending_for_me'] as num).toInt();
    return Panel(
      onTap: onTap,
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Expanded(child: Text(g['group_name'] as String, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600))),
          if (pending > 0) Tag('$pending to vote', Brand.caution) else Tag(roleName(g['role'] as String), Brand.muted),
        ]),
        const SizedBox(height: 14),
        Text(money(g['balance'] as num, cur), style: const TextStyle(fontSize: 30, fontWeight: FontWeight.w600)),
        const Text('Group balance', style: TextStyle(color: Brand.muted, fontSize: 13)),
        const SizedBox(height: 12),
        Row(children: [
          const Icon(Icons.savings_outlined, size: 18, color: Brand.safe),
          const SizedBox(width: 8),
          Text('You have saved ${money(g['my_contributions'] as num, cur)}', style: const TextStyle(color: Brand.text)),
        ]),
      ]),
    );
  }
}

class _ErrorBox extends StatelessWidget {
  const _ErrorBox(this.message, {required this.onRetry});
  final String message;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) => Panel(
        accent: Brand.alert,
        child: Row(children: [
          Expanded(child: Text(message)),
          TextButton(onPressed: onRetry, child: const Text('Try again')),
        ]),
      );
}
