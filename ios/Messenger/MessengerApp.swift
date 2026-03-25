import SwiftUI
import UserNotifications

@main
struct MessengerApp: App {

    @StateObject private var appState = AppState()

    init() {
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(appState)
                .onAppear { NotificationDelegate.shared.appState = appState }
        }
    }
}

/// Shows notification banners even when the app is in the foreground,
/// and opens the relevant chat when a notification is tapped.
final class NotificationDelegate: NSObject, UNUserNotificationCenterDelegate {
    static let shared = NotificationDelegate()
    weak var appState: AppState?

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        willPresent notification: UNNotification,
        withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void
    ) {
        completionHandler([.banner, .sound])
    }

    func userNotificationCenter(
        _ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse,
        withCompletionHandler completionHandler: @escaping () -> Void
    ) {
        if let senderKey = response.notification.request.content.userInfo["sender_key"] as? String {
            DispatchQueue.main.async {
                self.appState?.pendingOpenContactKey = senderKey
            }
        }
        completionHandler()
    }
}

/// Decides whether to show the onboarding flow or the main interface.
private struct RootView: View {
    @EnvironmentObject private var appState: AppState

    var body: some View {
        if appState.isOnboarded {
            ContentView()
                .onAppear { appState.setup() }
        } else {
            OnboardingView()
        }
    }
}
