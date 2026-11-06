import 'package:flutter/material.dart';

import '../api.dart';
import '../format.dart';
import '../theme.dart';
import 'shield_mark.dart';

/// First launch: connect the app to your ChamaChain server.
class ServerScreen extends StatefulWidget {
  const ServerScreen({super.key, required this.api, required this.onConnected});
  final Api api;
  final VoidCallback onConnected;

  @override
  State<ServerScreen> createState() => _ServerScreenState();
}

class _ServerScreenState extends State<ServerScreen> {
  final _address = TextEditingController(text: 'https://');
  bool _busy = false;
  String? _error;

  Future<void> _connect() async {
    final problem = checkServerAddress(_address.text);
    if (problem != null) {
      setState(() => _error = problem);
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.setServer(_address.text);
      widget.onConnected();
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } catch (_) {
      setState(() => _error = 'Cannot reach that address. Check it and your internet connection.');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  void dispose() {
    _address.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.fromLTRB(24, 48, 24, 24),
          children: [
            const ShieldMark(size: 56),
            const SizedBox(height: 24),
            const Text('Connect to your ChamaChain', style: TextStyle(fontSize: 26, fontWeight: FontWeight.w300)),
            const SizedBox(height: 8),
            const Text(
              'Enter the server address your group officials gave you. You only need to do this once.',
              style: TextStyle(color: Brand.muted, fontSize: 15, height: 1.4),
            ),
            const SizedBox(height: 28),
            TextField(
              controller: _address,
              keyboardType: TextInputType.url,
              autocorrect: false,
              decoration: field('Server address', helper: _error == null ? 'Example: https://app.chamachain.co.ke' : null).copyWith(errorText: _error),
              onSubmitted: (_) => _connect(),
            ),
            const SizedBox(height: 20),
            FilledButton(
              onPressed: _busy ? null : _connect,
              child: _busy ? const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.4)) : const Text('Connect'),
            ),
          ],
        ),
      ),
    );
  }
}
