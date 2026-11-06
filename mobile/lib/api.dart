import 'dart:async';
import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;

class ApiException implements Exception {
  ApiException(this.status, this.message);
  final int status;
  final String message;

  bool get isUnauthorized => status == 401;

  @override
  String toString() => message;
}

/// Talks to the ChamaChain API. The session token is kept in the phone's encrypted storage.
class Api {
  Api._(this._storage);

  static const _kServer = 'server';
  static const _kToken = 'token';
  static const _kPhone = 'phone';

  final FlutterSecureStorage _storage;
  final http.Client _http = http.Client();

  String server = '';
  String? token;
  String phone = '';

  /// Called when the server says the session has ended.
  void Function()? onSignedOut;

  static Future<Api> load() async {
    const storage = FlutterSecureStorage(aOptions: AndroidOptions(encryptedSharedPreferences: true));
    final api = Api._(storage);
    api.server = await storage.read(key: _kServer) ?? '';
    api.token = await storage.read(key: _kToken);
    api.phone = await storage.read(key: _kPhone) ?? '';
    return api;
  }

  bool get hasServer => server.isNotEmpty;
  bool get signedIn => token != null && token!.isNotEmpty;

  Future<void> setServer(String address) async {
    var a = address.trim();
    while (a.endsWith('/')) {
      a = a.substring(0, a.length - 1);
    }
    final res = await _http.get(Uri.parse('$a/health')).timeout(const Duration(seconds: 10));
    if (res.statusCode != 200) {
      throw ApiException(res.statusCode, 'That address did not answer like a ChamaChain server.');
    }
    server = a;
    await _storage.write(key: _kServer, value: a);
  }

  Future<void> forgetServer() async {
    await signOut();
    server = '';
    await _storage.delete(key: _kServer);
  }

  // ---------- sign-in ----------

  Future<String> requestCode(String phoneNumber) async {
    final data = await post('/auth/otp/request', {'phone': phoneNumber}, auth: false);
    phone = phoneNumber;
    await _storage.write(key: _kPhone, value: phoneNumber);
    return (data['message'] as String?) ?? 'Code sent.';
  }

  Future<void> verifyCode(String phoneNumber, String code) async {
    final data = await post('/auth/otp/verify', {'phone': phoneNumber, 'code': code, 'device': 'ChamaChain Android app'}, auth: false);
    token = data['token'] as String;
    await _storage.write(key: _kToken, value: token);
  }

  Future<void> signOut() async {
    if (signedIn) {
      try {
        await post('/me/logout', {});
      } catch (_) {}
    }
    token = null;
    await _storage.delete(key: _kToken);
  }

  // ---------- member data ----------

  Future<Map<String, dynamic>> home() async => await get('/me') as Map<String, dynamic>;
  Future<Map<String, dynamic>> group(String id) async => await get('/me/groups/$id') as Map<String, dynamic>;
  Future<List<dynamic>> activity(String id) async => await get('/me/groups/$id/activity') as List<dynamic>;
  Future<List<dynamic>> withdrawals(String id) async => await get('/me/groups/$id/withdrawals') as List<dynamic>;
  Future<Map<String, dynamic>> verify(String id) async => await get('/me/groups/$id/verify') as Map<String, dynamic>;
  Future<List<dynamic>> approvals() async => await get('/me/approvals') as List<dynamic>;

  Future<Map<String, dynamic>> contribute(String groupId, int amount) async =>
      await post('/me/groups/$groupId/contributions', {'amount': amount});

  Future<Map<String, dynamic>> requestWithdrawal(String groupId, int amount, String recipient, String reason) async =>
      await post('/me/groups/$groupId/withdrawals', {'amount': amount, 'recipient_phone': recipient, 'reason': reason});

  Future<Map<String, dynamic>> vote(String withdrawalId, bool approve) async =>
      await post('/me/withdrawals/$withdrawalId/${approve ? 'approve' : 'reject'}', {});

  Future<Map<String, dynamic>> cancel(String withdrawalId) async => await post('/me/withdrawals/$withdrawalId/cancel', {});

  // ---------- transport ----------

  Map<String, String> _headers(bool auth) => {
        'Content-Type': 'application/json',
        'Accept': 'application/json',
        if (auth && token != null) 'Authorization': 'Bearer $token',
      };

  Future<dynamic> get(String path) async {
    final res = await _send(() => _http.get(Uri.parse('$server$path'), headers: _headers(true)));
    return _decode(res);
  }

  Future<Map<String, dynamic>> post(String path, Map<String, dynamic> body, {bool auth = true}) async {
    final res = await _send(() => _http.post(Uri.parse('$server$path'), headers: _headers(auth), body: jsonEncode(body)));
    final data = _decode(res);
    return data is Map<String, dynamic> ? data : <String, dynamic>{};
  }

  Future<http.Response> _send(Future<http.Response> Function() call) async {
    try {
      return await call().timeout(const Duration(seconds: 20));
    } on TimeoutException {
      throw ApiException(0, 'The server took too long to answer. Check your connection and try again.');
    } catch (e) {
      if (e is ApiException) rethrow;
      throw ApiException(0, 'Cannot reach the ChamaChain server. Check your internet connection.');
    }
  }

  dynamic _decode(http.Response res) {
    dynamic data;
    try {
      data = res.body.isEmpty ? null : jsonDecode(res.body);
    } catch (_) {
      data = null;
    }
    if (res.statusCode >= 200 && res.statusCode < 300) return data;
    var message = 'Something went wrong (${res.statusCode}).';
    if (data is Map && data['error'] is String) message = _clean(data['error'] as String);
    if (res.statusCode == 401 && token != null) {
      token = null;
      _storage.delete(key: _kToken);
      onSignedOut?.call();
    }
    throw ApiException(res.statusCode, message);
  }

  static String _clean(String s) {
    final m = RegExp(r'^(validation failed|conflict|forbidden|not found|unauthorized): ?', caseSensitive: false);
    final t = s.replaceFirst(m, '');
    return t.isEmpty ? s : t[0].toUpperCase() + t.substring(1);
  }
}
