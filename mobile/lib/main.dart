import 'package:flutter/material.dart';

import 'api.dart';
import 'screens/home_screen.dart';
import 'screens/login_screen.dart';
import 'screens/server_screen.dart';
import 'theme.dart';

Future<void> main() async {
  WidgetsFlutterBinding.ensureInitialized();
  final api = await Api.load();
  runApp(ChamaChainApp(api: api));
}

class ChamaChainApp extends StatefulWidget {
  const ChamaChainApp({super.key, required this.api});
  final Api api;

  @override
  State<ChamaChainApp> createState() => _ChamaChainAppState();
}

class _ChamaChainAppState extends State<ChamaChainApp> {
  final _navigator = GlobalKey<NavigatorState>();

  /// Bumped whenever the sign-in state changes, so the first screen is chosen again.
  final _authState = ValueNotifier<int>(0);

  void _refresh() => _authState.value++;

  @override
  void initState() {
    super.initState();
    widget.api.onSignedOut = () {
      _navigator.currentState?.popUntil((r) => r.isFirst);
      _refresh();
    };
  }

  @override
  void dispose() {
    _authState.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final api = widget.api;
    return MaterialApp(
      title: 'ChamaChain',
      navigatorKey: _navigator,
      debugShowCheckedModeBanner: false,
      theme: buildTheme(),
      home: ValueListenableBuilder<int>(
        valueListenable: _authState,
        builder: (context, _, __) {
          if (!api.hasServer) return ServerScreen(api: api, onConnected: _refresh);
          if (!api.signedIn) return LoginScreen(api: api, onSignedIn: _refresh, onChangeServer: _refresh);
          return HomeScreen(api: api, onSignedOut: _refresh);
        },
      ),
    );
  }
}
