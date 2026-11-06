# ChamaChain member app (Android)

What members can do:

- Sign in with their phone number and a one-time SMS code (no passwords to forget)
- See each group's balance, what they have saved, and whether the records are verified against the blockchain
- Contribute through an M-Pesa prompt
- Request money, see how many approvals it has, and cancel it
- Approve or reject other members' requests, with the AI fraud check's reasons shown

## Get the app without installing anything (recommended)

GitHub builds the app for you, for free:

1. Put the `chamachain` folder in a **private** GitHub repository (see the main README, "Back up to GitHub").
2. On GitHub, open the repository, then **Actions**, then **Member app**. A build starts automatically whenever `mobile/` changes; you can also press **Run workflow**.
3. When the run shows a green tick (about 10 minutes), open it and download **chamachain-member-apk** under **Artifacts**. Unzip it to get `app-release.apk`.
4. Copy the APK to an Android phone and open it. Allow "install unknown apps" when Android asks.

The same run also checks the code (`flutter analyze`) and runs the tests, so a broken change never reaches members.

## Build it on your own computer (optional)

Needs about 10 GB: Flutter (https://docs.flutter.dev/get-started/install/linux/android) and Android Studio.

```bash
cd mobile
bash tool/prepare_android.sh
flutter pub get
flutter test
flutter build apk --release
# The app is at build/app/outputs/flutter-apk/app-release.apk
```

## Connect the app to ChamaChain

On first launch the app asks for the server address:

- **When ChamaChain is online:** `https://your-domain` (see the main README, "Going online").
- **To test with your laptop over Wi-Fi:** in `.env` set `API_BIND=0.0.0.0`, run `make up`, then in the app enter `http://<your-laptop-ip>:47080` (find the IP with `hostname -I`). Only do this on a Wi-Fi network you trust. The app only allows plain `http://` for addresses on a local network.

While SMS is in test mode, login codes are not texted. Run `make code` on the server to see them.

## Before the Play Store

The cloud build is signed with a development key, which is fine for installing directly on pilot members' phones. For Google Play you need your own upload key and a Google Play developer account (one-time USD 25). Follow https://docs.flutter.dev/deployment/android when you are ready.
