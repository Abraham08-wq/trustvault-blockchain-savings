import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../api.dart';
import '../format.dart';
import '../theme.dart';
import 'shield_mark.dart';

/// Sign in with your phone number and a one-time SMS code.
class LoginScreen extends StatefulWidget {
  const LoginScreen({super.key, required this.api, required this.onSignedIn, required this.onChangeServer});
  final Api api;
  final VoidCallback onSignedIn;
  final VoidCallback onChangeServer;

  @override
  State<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends State<LoginScreen> {
  late final _phone = TextEditingController(text: widget.api.phone);
  final _code = TextEditingController();
  bool _codeSent = false;
  bool _busy = false;
  String? _error;
  String? _info;

  Future<void> _sendCode() async {
    if (!looksLikePhone(_phone.text)) {
      setState(() => _error = 'Enter your phone number, for example 0712 345 678');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final msg = await widget.api.requestCode(_phone.text.trim());
      setState(() {
        _codeSent = true;
        _info = msg;
      });
    } on ApiException catch (e) {
      setState(() => _error = e.message);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _verify() async {
    if (_code.text.trim().length != 6) {
      setState(() => _error = 'Enter the 6-digit code from the SMS');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await widget.api.verifyCode(_phone.text.trim(), _code.text.trim());
      widget.onSignedIn();
    } on ApiException catch (e) {
      setState(() => _error = e.message);
      _code.clear();
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _changeServer() async {
    await widget.api.forgetServer();
    widget.onChangeServer();
  }

  @override
  void dispose() {
    _phone.dispose();
    _code.dispose();
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
            Text(_codeSent ? 'Enter your code' : 'Sign in', style: const TextStyle(fontSize: 28, fontWeight: FontWeight.w300)),
            const SizedBox(height: 8),
            Text(
              _codeSent
                  ? (_info ?? 'We sent a 6-digit code to your phone.')
                  : 'Use the phone number registered with your savings group. We will text you a one-time code.',
              style: const TextStyle(color: Brand.muted, fontSize: 15, height: 1.4),
            ),
            const SizedBox(height: 28),
            if (!_codeSent) ...[
              TextField(
                controller: _phone,
                keyboardType: TextInputType.phone,
                autofillHints: const [AutofillHints.telephoneNumber],
                decoration: field('Phone number', hint: '0712 345 678').copyWith(errorText: _error),
                onSubmitted: (_) => _sendCode(),
              ),
              const SizedBox(height: 20),
              FilledButton(onPressed: _busy ? null : _sendCode, child: _busy ? const _Spinner() : const Text('Send code')),
            ] else ...[
              TextField(
                controller: _code,
                autofocus: true,
                keyboardType: TextInputType.number,
                autofillHints: const [AutofillHints.oneTimeCode],
                inputFormatters: [FilteringTextInputFormatter.digitsOnly, LengthLimitingTextInputFormatter(6)],
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 28, letterSpacing: 12, fontWeight: FontWeight.w600),
                decoration: field('6-digit code').copyWith(errorText: _error),
                onChanged: (v) {
                  if (v.length == 6 && !_busy) _verify();
                },
              ),
              const SizedBox(height: 20),
              FilledButton(onPressed: _busy ? null : _verify, child: _busy ? const _Spinner() : const Text('Sign in')),
              const SizedBox(height: 8),
              TextButton(
                onPressed: _busy
                    ? null
                    : () => setState(() {
                          _codeSent = false;
                          _code.clear();
                          _error = null;
                        }),
                child: const Text('Use a different number or resend the code'),
              ),
            ],
            const SizedBox(height: 32),
            const Text(
              'ChamaChain will never ask for your M-Pesa PIN or this code by phone call or message. Do not share them with anyone, including group officials.',
              style: TextStyle(color: Brand.muted, fontSize: 13, height: 1.4),
            ),
            const SizedBox(height: 16),
            TextButton(onPressed: _busy ? null : _changeServer, child: const Text('Change server address')),
          ],
        ),
      ),
    );
  }
}

class _Spinner extends StatelessWidget {
  const _Spinner();
  @override
  Widget build(BuildContext context) => const SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.4));
}
