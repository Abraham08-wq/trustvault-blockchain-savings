import 'package:flutter_test/flutter_test.dart';

import 'package:chamachain_member/format.dart';

void main() {
  test('money groups thousands', () {
    expect(money(17000, 'KES'), 'KES 17,000');
    expect(money(1234567, 'UGX'), 'UGX 1,234,567');
    expect(money(0, 'KES'), 'KES 0');
    expect(groupDigits(-3000), '-3,000');
  });

  test('phone numbers', () {
    expect(looksLikePhone('0712 345 678'), isTrue);
    expect(looksLikePhone('+254712345678'), isTrue);
    expect(looksLikePhone('12ab'), isFalse);
  });

  test('server addresses must be https unless on the local network', () {
    expect(checkServerAddress('https://app.example.com'), isNull);
    expect(checkServerAddress('http://192.168.1.20:47080'), isNull);
    expect(checkServerAddress('http://10.0.0.5:47080'), isNull);
    expect(checkServerAddress('http://app.example.com'), isNotNull);
    expect(checkServerAddress('example.com'), isNotNull);
  });

  test('roles', () {
    expect(roleName('treasurer'), 'Treasurer');
    expect(roleName('anything'), 'Member');
  });
}
