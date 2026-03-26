import SwiftUI
import UIKit
import UserNotifications

@main
struct MessengerApp: App {

    @UIApplicationDelegateAdaptor(AppDelegate.self) var appDelegate
    @StateObject private var appState = AppState()

    init() {
        UNUserNotificationCenter.current().delegate = NotificationDelegate.shared
    }

    var body: some Scene {
        WindowGroup {
            RootView()
                .environmentObject(appState)
                .onAppear {
                    NotificationDelegate.shared.appState = appState
                    appDelegate.appState = appState
                }
        }
    }
}

/// App delegate that handles APNs device token registration.
final class AppDelegate: NSObject, UIApplicationDelegate {
    weak var appState: AppState?

    func application(_ application: UIApplication,
                     didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        appState?.registerDeviceToken(deviceToken)
    }

    func application(_ application: UIApplication,
                     didFailToRegisterForRemoteNotificationsWithError error: Error) {
        print("APNs registration failed: \(error)")
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
