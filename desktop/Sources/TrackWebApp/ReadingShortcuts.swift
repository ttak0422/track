import Foundation
import WebKit

/// A read-only host marker: no message handler or native API is exposed to page JavaScript.
/// Main-frame and exact-origin checks keep embedded pages and static apps outside this scope.
@MainActor
func installReadingShortcutsMarker(in configuration: WKWebViewConfiguration, workspaceOrigin: URL) {
    guard let data = try? JSONSerialization.data(withJSONObject: [workspaceOrigin.absoluteString]),
          let origins = String(data: data, encoding: .utf8) else { return }
    let source = """
    if (\(origins).includes(location.origin)) {
      Object.defineProperty(window, '__trackNativeReading', { value: true });
    }
    """
    configuration.userContentController.addUserScript(WKUserScript(
        source: source, injectionTime: .atDocumentStart, forMainFrameOnly: true
    ))
}
