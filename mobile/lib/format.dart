/// Formatting helpers with no dependencies, so they are easy to test.
library;

/// 1234567 -> "1,234,567"
String groupDigits(num value) {
  final negative = value < 0;
  final digits = value.abs().round().toString();
  final out = StringBuffer();
  for (var i = 0; i < digits.length; i++) {
    if (i > 0 && (digits.length - i) % 3 == 0) out.write(',');
    out.write(digits[i]);
  }
  return negative ? '-$out' : out.toString();
}

/// money(17000, "KES") -> "KES 17,000"
String money(num value, String currency) => '$currency ${groupDigits(value)}';

/// "2026-10-29T19:30:00Z" -> "29 Oct, 19:30" in local time
String shortDate(String iso) {
  final t = DateTime.tryParse(iso)?.toLocal();
  if (t == null) return '';
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec'];
  final hh = t.hour.toString().padLeft(2, '0');
  final mm = t.minute.toString().padLeft(2, '0');
  return '${t.day} ${months[t.month - 1]}, $hh:$mm';
}

String roleName(String role) {
  switch (role) {
    case 'chair':
      return 'Chair';
    case 'treasurer':
      return 'Treasurer';
    case 'secretary':
      return 'Secretary';
    default:
      return 'Member';
  }
}

/// Accepts 07XXXXXXXX, +2547XXXXXXXX or 2547XXXXXXXX style numbers.
bool looksLikePhone(String input) {
  final p = input.replaceAll(RegExp(r'[\s-]'), '');
  return RegExp(r'^\+?[0-9]{9,15}$').hasMatch(p);
}

/// Server addresses must use HTTPS, except computers on your own local network while testing.
String? checkServerAddress(String input) {
  final uri = Uri.tryParse(input.trim());
  if (uri == null || !uri.hasScheme || uri.host.isEmpty) {
    return 'Enter a full address, for example https://app.yourdomain.com';
  }
  if (uri.scheme == 'https') return null;
  if (uri.scheme == 'http' && isPrivateHost(uri.host)) return null;
  return 'Use an https:// address. Plain http is only allowed for a computer on your own Wi-Fi.';
}

bool isPrivateHost(String host) {
  if (host == 'localhost' || host == '10.0.2.2') return true;
  final parts = host.split('.').map(int.tryParse).toList();
  if (parts.length != 4 || parts.any((p) => p == null)) return false;
  final a = parts[0]!, b = parts[1]!;
  return a == 10 || (a == 192 && b == 168) || (a == 172 && b >= 16 && b <= 31) || a == 127;
}
