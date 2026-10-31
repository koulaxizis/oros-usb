// orOS USB: a System button in orOS's top bar with Wi-Fi, sound, battery,
// install to disk and power. It talks to the computer through bg.js and
// the oros-system native host.
//
// orOS Portable and the orOS desktop apps load the same extension; there
// the launcher is the native host. Its status says platform "portable"
// and lists in "caps" the parts this computer offers (sound, screen,
// power, sleep, exit, window, report, update, web, wifi); only those are
// shown, the power commands ask first, and the window can switch between
// full screen and a window. Without "caps" (orOS USB) everything stays
// as it was.
(() => {
  if (window.top !== window) return;

  // orOS's on-screen keyboard (osk.js) reads this: the kiosk has no system
  // keyboard, so on a touch screen it opens by itself and its top-bar
  // button is always there.
  document.documentElement.setAttribute("data-oros-kiosk", "usb");
  // orOS's Quick settings (qsettings.js) leaves its Network button to our
  // Wi-Fi button in its panel (see "Quick settings" below).
  document.documentElement.setAttribute("data-oros-qs-sys", "1");

  // orOS already is the system here: no browser "Install app" prompt (it
  // only opened an empty window in the kiosk).
  window.addEventListener("beforeinstallprompt", (e) => {
    e.preventDefault();
    e.stopImmediatePropagation();
  }, true);

  const TEXT = {
    en: {
      system: "System", wifi: "Wi-Fi", wired: "Wired network", connectedTo: "Connected to {n}",
      notConnected: "Not connected", wifiOff: "Wi-Fi is off", networks: "Show networks",
      searching: "Searching…", none: "No networks found", password: "Password", showPw: "Show password", hidePw: "Hide password", connect: "Connect",
      connecting: "Connecting…", connected: "Connected", sound: "Sound", mute: "Mute", unmute: "Unmute",
      noSound: "No sound device found", battery: "Battery {p}%", charging: "charging",
      install: "Install orOS on this computer", installTitle: "Install orOS on this computer",
      installText: "orOS will start from this disk without the USB stick. Everything on the disk you choose is erased: other systems, files and settings.",
      noDisks: "No internal disk of 8 GB or more was found.",
      keepData: "Copy my orOS data from this stick", understand: "I understand that everything on this disk will be erased",
      installNow: "Erase and install", back: "Back", installing: "Installing… do not turn off the computer.",
      installed: "orOS is installed. Shut down, remove the USB stick, then turn the computer on.",
      failed: "Install failed: {e}", restart: "Restart", shutdown: "Shut down", close: "Close",
      disks: "Disks", downloads: "Downloads", diskInternal: "This computer", diskUsb: "USB",
      diskOpen: "Open", diskClose: "Close", diskNone: "No other disks. A USB stick appears here when you plug it in.",
      disksHint: "Disks open read-only in the Internet window. orOS's own files are in the Files app.",
      disksHintFiles: "Disks open in Files › This computer. Windows disks are read-only.", diskReadOnly: "read-only",
      report: "Problem report", reportMaking: "Preparing the report…", reportSaved: "Saved in Downloads: {f}. You can attach it to a message from the Internet window.",
      update: "System update", updateCheck: "Check for updates", updateNone: "This is the newest system ({v}).",
      updateAvail: "New system {v} ({mb} MB). Your files and settings stay.", updateNow: "Download and install",
      updating: "Downloading the new system… {p}%", updated: "Done. Restart to start the new system.",
      updateNoPersist: "Updates need a persistence partition on the stick.",
      error: "Error: {e}", web: "Internet", webTitle: "Open a web browser window (close it to return to orOS)",
      tor: "Tor", torTitle: "Open Tor Browser for private browsing (close it to return to orOS)",
      torAsk: "The first time, Tor Browser (about 110 MB) is downloaded from torproject.org and checked. Continue?",
      torAskNoPersist: "This stick has no persistence partition, so Tor Browser is downloaded again after every restart.",
      torGetting: "Getting Tor Browser… {p}%", torError: "Tor Browser could not be set up: {e}",
      screen: "Screen", brightness: "Brightness", time: "Time", timeZone: "Time zone",
      timeSynced: "The clock is set from the internet.", timeNotSynced: "The clock is set from the internet once you are online.",
      timeZoneInstall: "Time zone of this computer",
      bt: "Bluetooth", btFind: "Find devices", btSearching: "Searching for 10 seconds… put the device in pairing mode.",
      btNone: "No new devices found. Is the device in pairing mode?", btPair: "Pair", btPairing: "Pairing…",
      btPasskey: "Type {k} on the keyboard, then press Enter.", btConnect: "Connect", btDisconnect: "Disconnect",
      btForget: "Forget", btConnected: "connected", btNoPaired: "No paired devices.", btOff: "Bluetooth is off",
      printers: "Printers", printTest: "Test page", printSent: "Test page sent to {p}.",
      printNone: "No printers yet. Printers that support AirPrint or IPP Everywhere appear by themselves when they are on the same network or plugged in by USB.",
      printHint: "Print from any app with Ctrl+P.",
      qsReboot: "Restart the computer", qsMore: "Printers, disks, update…", volume: "Volume",
      bridgeCode: "orOS Bridge pairing code: type it in orOS Studio",
    },
    el: {
      system: "Σύστημα", wifi: "Wi-Fi", wired: "Ενσύρματο δίκτυο", connectedTo: "Συνδεδεμένο στο {n}",
      notConnected: "Χωρίς σύνδεση", wifiOff: "Το Wi-Fi είναι κλειστό", networks: "Εμφάνιση δικτύων",
      searching: "Αναζήτηση…", none: "Δεν βρέθηκαν δίκτυα", password: "Κωδικός", showPw: "Εμφάνιση κωδικού", hidePw: "Απόκρυψη κωδικού", connect: "Σύνδεση",
      connecting: "Σύνδεση…", connected: "Συνδέθηκε", sound: "Ήχος", mute: "Σίγαση", unmute: "Ήχος ανοιχτός",
      noSound: "Δεν βρέθηκε συσκευή ήχου", battery: "Μπαταρία {p}%", charging: "φορτίζει",
      install: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή", installTitle: "Εγκατάσταση του orOS σε αυτόν τον υπολογιστή",
      installText: "Το orOS θα ξεκινά από αυτόν τον δίσκο χωρίς το USB stick. Ό,τι υπάρχει στον δίσκο που θα διαλέξεις σβήνεται: άλλα συστήματα, αρχεία και ρυθμίσεις.",
      noDisks: "Δεν βρέθηκε εσωτερικός δίσκος 8 GB ή μεγαλύτερος.",
      keepData: "Αντιγραφή των δεδομένων μου του orOS από αυτό το stick", understand: "Καταλαβαίνω ότι θα σβηστούν όλα όσα υπάρχουν σε αυτόν τον δίσκο",
      installNow: "Διαγραφή και εγκατάσταση", back: "Πίσω", installing: "Εγκατάσταση… μην κλείσεις τον υπολογιστή.",
      installed: "Το orOS εγκαταστάθηκε. Κλείσε τον υπολογιστή, βγάλε το USB stick και άνοιξέ τον ξανά.",
      failed: "Η εγκατάσταση απέτυχε: {e}", restart: "Επανεκκίνηση", shutdown: "Τερματισμός", close: "Κλείσιμο",
      disks: "Δίσκοι", downloads: "Λήψεις", diskInternal: "Αυτός ο υπολογιστής", diskUsb: "USB",
      diskOpen: "Άνοιγμα", diskClose: "Κλείσιμο", diskNone: "Δεν υπάρχουν άλλοι δίσκοι. Ένα USB stick εμφανίζεται εδώ μόλις το συνδέσεις.",
      disksHint: "Οι δίσκοι ανοίγουν μόνο για ανάγνωση στο παράθυρο Internet. Τα αρχεία του ίδιου του orOS είναι στην εφαρμογή Αρχεία.",
      disksHintFiles: "Οι δίσκοι ανοίγουν στα Αρχεία › Αυτός ο υπολογιστής. Οι δίσκοι των Windows είναι μόνο για ανάγνωση.", diskReadOnly: "μόνο ανάγνωση",
      report: "Αναφορά προβλήματος", reportMaking: "Ετοιμάζεται η αναφορά…", reportSaved: "Αποθηκεύτηκε στις Λήψεις: {f}. Μπορείς να το επισυνάψεις σε μήνυμα από το παράθυρο Internet.",
      update: "Ενημέρωση συστήματος", updateCheck: "Έλεγχος για ενημερώσεις", updateNone: "Έχεις το νεότερο σύστημα ({v}).",
      updateAvail: "Νέο σύστημα {v} ({mb} MB). Τα αρχεία και οι ρυθμίσεις σου μένουν.", updateNow: "Λήψη και εγκατάσταση",
      updating: "Λήψη του νέου συστήματος… {p}%", updated: "Έτοιμο. Κάνε επανεκκίνηση για να ξεκινήσει το νέο σύστημα.",
      updateNoPersist: "Οι ενημερώσεις χρειάζονται διαμέρισμα persistence στο stick.",
      error: "Σφάλμα: {e}", web: "Internet", webTitle: "Άνοιγμα παραθύρου για το διαδίκτυο (κλείσ' το για να γυρίσεις στο orOS)",
      tor: "Tor", torTitle: "Άνοιγμα του Tor Browser για ιδιωτική περιήγηση (κλείσ' το για να γυρίσεις στο orOS)",
      torAsk: "Την πρώτη φορά το Tor Browser (περίπου 110 MB) κατεβαίνει από το torproject.org και ελέγχεται. Συνέχεια;",
      torAskNoPersist: "Αυτό το stick δεν έχει persistence partition, οπότε το Tor Browser κατεβαίνει ξανά μετά από κάθε επανεκκίνηση.",
      torGetting: "Λήψη του Tor Browser… {p}%", torError: "Το Tor Browser δεν μπόρεσε να εγκατασταθεί: {e}",
      screen: "Οθόνη", brightness: "Φωτεινότητα", time: "Ώρα", timeZone: "Ζώνη ώρας",
      timeSynced: "Η ώρα ρυθμίζεται από το διαδίκτυο.", timeNotSynced: "Η ώρα θα ρυθμιστεί από το διαδίκτυο μόλις συνδεθείς.",
      timeZoneInstall: "Ζώνη ώρας αυτού του υπολογιστή",
      bt: "Bluetooth", btFind: "Εύρεση συσκευών", btSearching: "Αναζήτηση για 10 δευτερόλεπτα… βάλε τη συσκευή σε λειτουργία σύζευξης.",
      btNone: "Δεν βρέθηκαν νέες συσκευές. Είναι η συσκευή σε λειτουργία σύζευξης;", btPair: "Σύζευξη", btPairing: "Σύζευξη…",
      btPasskey: "Πληκτρολόγησε {k} στο πληκτρολόγιο και πάτα Enter.", btConnect: "Σύνδεση", btDisconnect: "Αποσύνδεση",
      btForget: "Διαγραφή", btConnected: "συνδεδεμένο", btNoPaired: "Δεν υπάρχουν συζευγμένες συσκευές.", btOff: "Το Bluetooth είναι κλειστό",
      printers: "Εκτυπωτές", printTest: "Δοκιμαστική σελίδα", printSent: "Η δοκιμαστική σελίδα στάλθηκε στον {p}.",
      printNone: "Δεν υπάρχουν ακόμη εκτυπωτές. Όσοι υποστηρίζουν AirPrint ή IPP Everywhere εμφανίζονται μόνοι τους όταν είναι στο ίδιο δίκτυο ή συνδεθούν με USB.",
      printHint: "Εκτύπωση από κάθε εφαρμογή με Ctrl+P.",
      qsReboot: "Επανεκκίνηση του υπολογιστή", qsMore: "Εκτυπωτές, δίσκοι, ενημέρωση…", volume: "Ένταση",
    },
  };
  Object.assign(TEXT.en, {
    exit: "Exit orOS", sleep: "Sleep", winFull: "Full screen", winWindow: "Show in a window",
    pcRestart: "Restart the computer", pcShutdown: "Shut down the computer", pcSleep: "Put the computer to sleep",
    sure: "{a}? Click again to confirm.", appUpdate: "orOS update",
    appUpdateAvail: "A newer orOS is ready to download ({v}). It opens the next time you start orOS.",
    appUpdating: "Downloading orOS… {mb} MB", appUpdated: "Done. Close orOS and start it again to use the new version.",
    appUpdateNone: "This is the newest orOS ({v}).",
    reportSavedPc: "Saved in this computer's Downloads folder: {f}.", qsMorePc: "Update, problem report…",
    btSettings: "Bluetooth settings of this computer", btSettingsSub: "Opens the computer's settings",
  });
  Object.assign(TEXT.el, {
    bridgeCode: "Κωδικός σύζευξης του orOS Bridge: πληκτρολόγησέ τον στο orOS Studio",
    exit: "Έξοδος από το orOS", sleep: "Αναστολή", winFull: "Πλήρης οθόνη", winWindow: "Εμφάνιση σε παράθυρο",
    pcRestart: "Επανεκκίνηση του υπολογιστή", pcShutdown: "Τερματισμός του υπολογιστή", pcSleep: "Αναστολή του υπολογιστή",
    sure: "{a}; Πάτα ξανά για επιβεβαίωση.", appUpdate: "Ενημέρωση του orOS",
    appUpdateAvail: "Υπάρχει νεότερο orOS για λήψη ({v}). Ανοίγει την επόμενη φορά που θα ξεκινήσεις το orOS.",
    appUpdating: "Λήψη του orOS… {mb} MB", appUpdated: "Έτοιμο. Κλείσε το orOS και άνοιξέ το ξανά για τη νέα έκδοση.",
    appUpdateNone: "Έχεις το νεότερο orOS ({v}).",
    reportSavedPc: "Αποθηκεύτηκε στις Λήψεις αυτού του υπολογιστή: {f}.", qsMorePc: "Ενημέρωση, αναφορά προβλήματος…",
    btSettings: "Ρυθμίσεις Bluetooth του υπολογιστή", btSettingsSub: "Ανοίγει τις ρυθμίσεις του υπολογιστή",
  });
  const lang = () => ((localStorage.getItem("oros-lang") || navigator.language || "en").slice(0, 2) === "el" ? "el" : "en");
  const t = (k, v) => {
    let s = TEXT[lang()][k] || TEXT.en[k] || k;
    for (const [a, b] of Object.entries(v || {})) s = s.replace("{" + a + "}", b);
    return s;
  };

  // bg.js asks after the browser starts, and loads the page again when
  // nobody answers (see reloadBare there).
  chrome.runtime.onMessage.addListener((msg, sender, send) => {
    if (msg && msg.cmd === "oros-here") send(true);
  });

  const call = (msg) => new Promise((resolve) => {
    try {
      chrome.runtime.sendMessage(msg, (res) => {
        const err = chrome.runtime.lastError;
        resolve(res || { ok: false, error: err ? err.message : "no answer" });
      });
    } catch (e) {
      resolve({ ok: false, error: String(e) });
    }
  });

  const ICON = {
    wifi: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 8.5a15 15 0 0 1 20 0"/><path d="M5 12a10.5 10.5 0 0 1 14 0"/><path d="M8.5 15.5a5.5 5.5 0 0 1 7 0"/><circle cx="12" cy="19" r="1" fill="currentColor"/></svg>',
    wifiOff: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 8.5a15 15 0 0 1 20 0" opacity=".35"/><path d="M5 12a10.5 10.5 0 0 1 14 0" opacity=".35"/><path d="M8.5 15.5a5.5 5.5 0 0 1 7 0" opacity=".35"/><path d="M3 3l18 18"/></svg>',
    wired: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="14" width="18" height="6" rx="1"/><path d="M7 14v-4h10v4M12 10V4"/></svg>',
    power: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3v9"/><path d="M6.3 7.3a8 8 0 1 0 11.4 0"/></svg>',
    onion: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 3c-1 2-1 3 0 4"/><path d="M12 7c4 2 7 5 7 8.5A7 6.5 0 0 1 5 15.5C5 12 8 9 12 7z"/><path d="M12 10c2 1.5 3.5 3.3 3.5 5.5S14 19 12 19s-3.5-1.3-3.5-3.5S10 11.5 12 10z"/></svg>',
    globe: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/></svg>',
    eye: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/></svg>',
    eyeOff: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/><path d="M3 3l18 18"/></svg>',
    drive: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="13" width="18" height="7" rx="2"/><path d="M5 13l2.5-8h9L19 13"/><circle cx="17" cy="16.5" r=".8" fill="currentColor"/></svg>',
    usb: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="7" y="9" width="10" height="13" rx="2"/><path d="M9 9V3h6v6M11 5.5h0M13 5.5h0"/></svg>',
    folder: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linejoin="round"><path d="M3 6.5A1.5 1.5 0 0 1 4.5 5H9l2 2h8.5A1.5 1.5 0 0 1 21 8.5v9a1.5 1.5 0 0 1-1.5 1.5h-15A1.5 1.5 0 0 1 3 17.5z"/></svg>',
    speaker: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 5L6 9H3v6h3l5 4z"/><path d="M15.5 8.5a5 5 0 0 1 0 7"/><path d="M18.5 5.5a9 9 0 0 1 0 13"/></svg>',
    speakerOff: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M11 5L6 9H3v6h3l5 4z"/><path d="M22 9l-6 6M16 9l6 6"/></svg>',
    sun: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4"/></svg>',
    bt: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 7l10 10-5 5V2l5 5L7 17"/></svg>',
    gear: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/></svg>',
    more: '<svg viewBox="0 0 24 24" width="18" height="18" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>',
    lock: '<svg viewBox="0 0 24 24" width="14" height="14" fill="none" stroke="currentColor" stroke-width="2"><rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/></svg>',
  };

  const CSS = `
    :host { all: initial; }
    .panel { position: fixed; top: 46px; right: 10px; width: 340px; max-height: calc(100vh - 60px); overflow: auto;
      background: #1f1d1a; color: #e8e4dc; border: 1px solid #3a352d; border-radius: 12px; padding: 14px;
      font: 14px/1.4 system-ui, "Noto Sans", sans-serif; box-shadow: 0 12px 40px rgba(0,0,0,.5); z-index: 2147483000; }
    h2 { font-size: 15px; margin: 0 0 10px; color: #c8a96e; font-weight: 600; }
    h3 { font-size: 12px; text-transform: uppercase; letter-spacing: .06em; color: #a69d8e; margin: 14px 0 6px; font-weight: 600; }
    .row { display: flex; align-items: center; gap: 8px; }
    .grow { flex: 1; }
    .muted { color: #a69d8e; font-size: 13px; }
    button { font: inherit; color: #e8e4dc; background: #2c2924; border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 10px; cursor: pointer; }
    button:hover { border-color: #c8a96e; }
    button.primary { background: #c8a96e; color: #1b1a18; border-color: #c8a96e; font-weight: 600; }
    button.danger { background: #8c2f2f; border-color: #8c2f2f; color: #fff; font-weight: 600; }
    button:disabled { opacity: .45; cursor: default; }
    .net { display: flex; align-items: center; gap: 8px; width: 100%; text-align: left; margin: 3px 0; background: #26231f; }
    .net.on { border-color: #c8a96e; }
    input[type=password], input[type=text] { font: inherit; width: 100%; box-sizing: border-box; background: #14120f; color: #e8e4dc;
      border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 9px; margin: 6px 0; }
    .disk { display: flex; align-items: center; gap: 8px; margin: 4px 0; }
    .disk .name { flex: 1; min-width: 0; }
    .disk .name div { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
    .disk button { padding: 5px 10px; }
    .pw { display: flex; align-items: center; gap: 6px; }
    .pw button { padding: 6px 8px; line-height: 0; }
    input[type=range] { width: 100%; accent-color: #c8a96e; }
    select { font: inherit; width: 100%; box-sizing: border-box; background: #14120f; color: #e8e4dc; border: 1px solid #3f3a32; border-radius: 8px; padding: 7px 8px; }
    .switch { position: relative; width: 40px; height: 22px; border-radius: 11px; background: #3f3a32; border: none; padding: 0; }
    .switch::after { content: ""; position: absolute; top: 3px; left: 3px; width: 16px; height: 16px; border-radius: 50%; background: #e8e4dc; transition: left .15s; }
    .switch.on { background: #c8a96e; }
    .switch.on::after { left: 21px; }
    .bar { height: 8px; background: #2c2924; border-radius: 4px; overflow: hidden; margin: 8px 0; }
    .bar > div { height: 100%; background: #c8a96e; width: 0; transition: width .4s; }
    label.chk { display: flex; gap: 8px; align-items: flex-start; margin: 8px 0; }
    .msg { margin: 6px 0; font-size: 13px; }
    .msg.err { color: #f0a0a0; }
    .power { display: flex; gap: 8px; margin-top: 14px; }
    [hidden] { display: none !important; }
    .power button { flex: 1; }
  `;

  let state = {};
  // orOS Portable / desktop apps: the parts this computer offers. orOS USB
  // sends no caps and has everything.
  const portable = () => state.platform === "portable";
  const has = (k) => !state.caps || !!state.caps[k];
  // The first status of a portable copy takes back what only the USB
  // kiosk wants (osk.js's always-on keyboard, the Network button's place).
  function applyPlatform() {
    // orOS's Settings › Report a problem offers "Save with this computer's
    // logs" while this is set (see "Problem report from orOS" below).
    if (has("report")) document.documentElement.setAttribute("data-oros-report", "1");
    else document.documentElement.removeAttribute("data-oros-report");
    if (!portable()) return;
    document.documentElement.removeAttribute("data-oros-kiosk");
    if (has("wifi")) document.documentElement.setAttribute("data-oros-qs-sys", "1");
    else document.documentElement.removeAttribute("data-oros-qs-sys");
    if (webBtn) webBtn.hidden = !has("web");
    if (torBtn) torBtn.hidden = !has("tor");
  }
  // A power command on a computer that is not orOS's own asks first: the
  // first click turns the button into "…? Click again", which stays for
  // five seconds.
  function confirmButton(b, label, run) {
    let armed = 0;
    b.textContent = label;
    b.addEventListener("click", (e) => {
      e.stopPropagation();
      if (!portable() || Date.now() - armed < 5000) { armed = 0; b.textContent = label; run(); return; }
      armed = Date.now();
      b.textContent = t("sure", { a: label });
      setTimeout(() => { if (armed && Date.now() - armed >= 5000) { armed = 0; b.textContent = label; } }, 5100);
    });
    return b;
  }
  // The window and leaving orOS go through bg.js (chrome.windows).
  const setWindow = (mode) => call({ cmd: "window.mode", mode }).then(refresh);
  const exitOros = () => call({ cmd: "session.exit" });
  const openBtSettings = () => { qsCloseShell(); call({ cmd: "bt.settings" }); };
  let webBtn = null, torBtn = null, torTimer = null;
  let btn = null, host = null, root = null, panel = null, view = "main", pollTimer = null;

  // setInterval for async checks: a tick waits while the last one is still
  // waiting for its answer, so a slow answer never runs the end twice.
  const poll = (fn, ms) => {
    let busy = false;
    return setInterval(async () => {
      if (busy) return;
      busy = true;
      try { await fn(); } finally { busy = false; }
    }, ms);
  };
  const el = (tag, attrs, ...kids) => {
    const n = document.createElement(tag);
    for (const [k, v] of Object.entries(attrs || {})) {
      if (k === "class") n.className = v;
      else if (k === "html") n.innerHTML = v;
      else if (k.startsWith("on")) n.addEventListener(k.slice(2), v);
      else if (v !== false && v != null) n.setAttribute(k, v === true ? "" : v);
    }
    for (const kid of kids) if (kid != null) n.append(kid);
    return n;
  };

  function paintButton() {
    if (!btn) return;
    let icon = has("wifi") ? ICON.wifiOff : ICON.gear;
    if (state.wired) icon = ICON.wired;
    else if (state.wifi) icon = ICON.wifi;
    let txt = "";
    if (state.battery) txt = " " + state.battery.percent + "%";
    btn.innerHTML = icon + '<span style="margin-left:4px">' + txt + "</span>";
    btn.title = t("system");
    // orOS's Quick settings shows the battery from this.
    if (state.battery) document.documentElement.setAttribute("data-oros-sys-battery", state.battery.percent + ":" + (state.battery.charging ? 1 : 0));
    else document.documentElement.removeAttribute("data-oros-sys-battery");
    qsStep();
  }

  // Problem report from orOS: Settings sends its own part (already shown
  // to the person) as a JSON string; the host adds this computer's logs
  // and saves one file in Downloads. Details are strings both ways, so
  // they cross between the page and this script unchanged.
  let reportBusy = false;
  document.addEventListener("oros-report", async (ev) => {
    const answer = (r) => document.dispatchEvent(new CustomEvent("oros-report-done", { detail: JSON.stringify(r) }));
    let web = "";
    try { web = String(JSON.parse(String(ev.detail)).text || ""); } catch (e) {}
    if (!web || !has("report")) { answer({ ok: false, error: "not available" }); return; }
    if (reportBusy) { answer({ ok: false, error: "busy" }); return; }
    reportBusy = true;
    const r = await call({ cmd: "report", web: web.slice(0, 512 * 1024) });
    reportBusy = false;
    answer(r.ok ? { ok: true, file: String(r.file || "") } : { ok: false, error: String(r.error || "error") });
  });

  async function refresh() {
    const r = await call({ cmd: "status" });
    if (r.ok) { state = r; applyPlatform(); }
    paintButton();
    if (panel && view === "main" && main) paintMain();
  }

  function close() {
    if (host) host.remove();
    host = root = panel = main = null;
    view = "main";
    clearInterval(pollTimer);
    document.removeEventListener("keydown", onKey, true);
    document.removeEventListener("mousedown", onOutside, true);
  }
  const onKey = (e) => { if (e.key === "Escape" && view !== "installing") close(); };
  const onOutside = (e) => {
    if (view === "installing") return;
    if (host && !e.composedPath().includes(host) && !e.composedPath().includes(btn)) close();
  };

  function open() {
    if (host) { close(); return; }
    host = el("div", { id: "oros-system-panel" });
    root = host.attachShadow({ mode: "open" });
    root.append(el("style", {}, CSS));
    panel = el("div", { class: "panel", role: "dialog", "aria-label": t("system") });
    root.append(panel);
    document.body.append(host);
    document.addEventListener("keydown", onKey, true);
    document.addEventListener("mousedown", onOutside, true);
    // Which panel to build depends on the edition: wait for the first
    // status if it has not come yet.
    if (state.ok) { renderMain(); refresh(); }
    else refresh().then(() => { if (panel) renderMain(); });
  }

  // The main view is built once per opening; refresh() only repaints its
  // status parts, so a scanned network list or a password being typed stays.
  let main = null;

  function renderMain() {
    view = "main";
    panel.replaceChildren();
    const wifiText = el("span", { class: "grow" });
    const sw = el("button", { class: "switch", "aria-label": t("wifi"),
      onclick: async () => { await call({ cmd: "wifi.radio", on: !state.wifiOn }); await refresh(); } });
    const list = el("div", {});
    const nets = el("div", {}, el("div", { class: "row", style: "margin-top:6px" },
      el("button", { onclick: () => scan(list) }, t("networks"))), list);
    const range = el("input", { type: "range", min: "0", max: "100", "aria-label": t("sound") });
    range.addEventListener("change", () => call({ cmd: "volume.set", value: Number(range.value) }).then(refresh));
    const mute = el("button", { onclick: () => call({ cmd: "volume.mute", on: !state.muted }).then(refresh) });
    const soundRow = el("div", { class: "row" }, el("div", { class: "grow" }, range), mute);
    const noSound = el("div", { class: "muted" }, t("noSound"));
    const battery = el("div", { class: "muted", style: "margin-top:12px" });
    // Brightness: only on screens with a backlight (laptops).
    const light = el("input", { type: "range", min: "5", max: "100", "aria-label": t("brightness") });
    // Follows the slider while it moves, one call at a time.
    let lightBusy = false;
    const sendLight = async () => {
      if (lightBusy) return;
      lightBusy = true;
      const v = light.value;
      await call({ cmd: "brightness.set", value: Number(v) });
      lightBusy = false;
      if (light.value !== v) sendLight();
    };
    light.addEventListener("input", sendLight);
    const screenBox = el("div", {}, el("h3", {}, t("screen")), el("div", { class: "row" }, el("span", {}, t("brightness")), el("div", { class: "grow" }, light)));
    const timeBox = el("div", {});
    const btBox = el("div", {});
    const printBox = el("div", {});
    const install = el("div", { style: "margin-top:14px" }, el("button", { style: "width:100%", onclick: renderInstall }, t("install")));
    const disksBox = el("div", {});
    const updateBox = el("div", {});
    const reportMsg = el("div", {});
    const reportBtn = el("button", { style: "width:100%", onclick: async () => {
      reportBtn.disabled = true;
      reportMsg.replaceChildren(el("div", { class: "muted" }, t("reportMaking")));
      const r = await call({ cmd: "report" });
      reportBtn.disabled = false;
      reportMsg.replaceChildren(el("div", { class: r.ok ? "msg" : "msg err" }, r.ok ? t(portable() ? "reportSavedPc" : "reportSaved", { f: r.file }) : t("error", { e: r.error })));
    } }, t("report"));
    if (portable()) {
      renderPortable({ wifiText, sw, nets, list, range, mute, soundRow, noSound, battery, light, screenBox, updateBox, reportBtn, reportMsg });
      return;
    }
    panel.append(el("h2", {}, t("system")),
      el("h3", {}, t("wifi")), el("div", { class: "row" }, wifiText, sw), nets,
      el("h3", {}, t("sound")), soundRow, noSound, battery, screenBox,
      btBox,
      el("h3", {}, t("printers")), printBox,
      el("h3", {}, t("time")), timeBox,
      el("h3", {}, t("disks")), disksBox,
      el("h3", {}, t("update")), updateBox, install,
      el("div", { style: "margin-top:10px" }, reportBtn, reportMsg),
      el("div", { class: "power" },
        el("button", { onclick: () => call({ cmd: "power.reboot" }) }, t("restart")),
        el("button", { class: "primary", html: ICON.power + " " + t("shutdown"), onclick: () => call({ cmd: "power.off" }) })));
    main = { wifiText, sw, nets, list, range, mute, soundRow, noSound, battery, install, light, screenBox };
    paintMain();
    showTime(timeBox, false);
    showBluetooth(btBox);
    showPrinters(printBox);
    showUpdate(updateBox);
    showDisks(disksBox);
  }

  // The panel of orOS Portable and the desktop apps: only what the
  // launcher offers on this computer. Bluetooth opens the computer's own
  // settings (pairing stays where it is safe and familiar).
  function renderPortable(p) {
    const parts = [el("h2", {}, t("system"))];
    if (has("wifi")) parts.push(el("h3", {}, t("wifi")), el("div", { class: "row" }, p.wifiText, p.sw), p.nets);
    if (has("btSettings")) {
      parts.push(el("h3", {}, t("bt")), el("button", { style: "width:100%", onclick: openBtSettings }, t("btSettings")));
    }
    if (has("sound")) parts.push(el("h3", {}, t("sound")), p.soundRow, p.noSound);
    parts.push(p.battery);
    if (has("screen")) parts.push(p.screenBox);
    if (has("update")) parts.push(el("h3", {}, t("appUpdate")), p.updateBox);
    if (has("report")) parts.push(el("div", { style: "margin-top:10px" }, p.reportBtn, p.reportMsg));
    const win = el("button", { onclick: () => setWindow(state.window === "fullscreen" ? "window" : "fullscreen") });
    const row1 = el("div", { class: "power" });
    if (has("window")) row1.append(win);
    if (has("exit")) row1.append(el("button", { class: "primary", html: ICON.power + " " + t("exit"), onclick: exitOros }));
    const row2 = el("div", { class: "power" });
    if (has("power") && has("sleep")) row2.append(confirmButton(el("button", {}), t("sleep"), () => call({ cmd: "power.sleep" })));
    if (has("power")) {
      row2.append(confirmButton(el("button", {}), t("restart"), () => call({ cmd: "power.reboot" })),
        confirmButton(el("button", {}), t("shutdown"), () => call({ cmd: "power.off" })));
    }
    parts.push(row1, row2);
    panel.append(...parts);
    main = { range: p.range, mute: p.mute, soundRow: p.soundRow, noSound: p.noSound, battery: p.battery,
      light: p.light, screenBox: p.screenBox, win, portable: true,
      wifiText: p.wifiText, sw: p.sw, nets: p.nets, list: p.list };
    paintMain();
    if (has("update")) showUpdate(p.updateBox);
  }

  // Disks: Downloads, the computer's own disks and USB sticks. Opening one
  // mounts it read-only and shows it in the Internet window. The list
  // follows sticks being plugged in while the panel is open.
  function showDisks(box) {
    const list = el("div", {});
    const msg = el("div", {});
    // orOS with "This computer" in Files (4.x, wave 3 of the computer files
    // plan) marks the page; disks then open there instead of the Internet
    // window.
    const inFiles = () => document.documentElement.getAttribute("data-oros-computer-files") === "1";
    box.replaceChildren(list, msg, el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, t(inFiles() ? "disksHintFiles" : "disksHint")));
    const gb = (n) => { const g = n / 1e9; return (g < 10 ? g.toFixed(1) : Math.round(g)) + " GB"; };
    const row = (icon, title, sub, ...buttons) => el("div", { class: "disk" }, el("span", { html: icon }),
      el("div", { class: "name" }, el("div", {}, title), sub ? el("div", { class: "muted", style: "font-size:12px" }, sub) : null), ...buttons);
    const act = async (cmd, path) => {
      msg.replaceChildren();
      const files = cmd === "disks.open" && inFiles();
      const r = await call({ cmd: files ? "disks.mount" : cmd, disk: path });
      if (!r.ok) msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error })));
      else if (files && r.mount) {
        // orOS Files opens this place ("/home/..." or "/disks/<name>").
        const at = r.mount.startsWith("/media/oros/") ? "/disks/" + r.mount.slice(12) : "/home/Downloads";
        document.dispatchEvent(new CustomEvent("oros-files-open", { detail: at }));
      }
      last = "";
      load();
    };
    let last = "";
    async function load() {
      if (!box.isConnected) { clearInterval(timer); return; }
      const r = await call({ cmd: "disks.list" });
      const key = JSON.stringify(r);
      if (key === last) return;
      last = key;
      const rows = [row(ICON.folder, t("downloads"), "", el("button", { onclick: () => act("disks.open", "downloads") }, t("diskOpen")))];
      if (!r.ok) rows.push(el("div", { class: "msg err" }, t("error", { e: r.error })));
      for (const v of (r.volumes || [])) {
        const where = v.usb ? t("diskUsb") : t("diskInternal");
        const title = v.label || v.model || v.path.replace("/dev/", "");
        const sub = [where, title === v.model ? "" : v.model, gb(v.size), v.fstype.toUpperCase(), v.readonly ? t("diskReadOnly") : ""].filter(Boolean).join(" · ");
        const buttons = [el("button", { onclick: () => act("disks.open", v.path) }, t("diskOpen"))];
        if (v.mount) buttons.push(el("button", { onclick: () => act("disks.close", v.path) }, t("diskClose")));
        rows.push(row(v.usb ? ICON.usb : ICON.drive, title, sub, ...buttons));
      }
      if (r.ok && !(r.volumes || []).length) rows.push(el("div", { class: "muted" }, t("diskNone")));
      list.replaceChildren(...rows);
    }
    const timer = setInterval(load, 4000);
    load();
  }

  // Time zone: kept on the stick, copied when orOS is installed. The clock
  // itself comes from the internet (NTP).
  async function showTime(box, forInstall) {
    const r = await call({ cmd: "time.get" });
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    const sel = el("select", { "aria-label": t("timeZone") });
    for (const z of r.zones || []) sel.append(el("option", { value: z, selected: z === r.zone }, z.replace(/_/g, " ")));
    const msg = el("div", {});
    sel.addEventListener("change", async () => {
      msg.replaceChildren();
      const s = await call({ cmd: "time.zone", zone: sel.value });
      if (!s.ok) msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e: s.error })));
    });
    box.replaceChildren(el("div", { class: forInstall ? "" : "muted", style: "margin-bottom:4px" }, forInstall ? t("timeZoneInstall") : t("timeZone")), sel, msg);
    if (!forInstall) box.append(el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, r.synced ? t("timeSynced") : t("timeNotSynced")));
  }

  // Bluetooth: paired devices with connect / disconnect / forget, and a
  // search that pairs new ones (showing the code a keyboard asks for).
  // Hidden on computers without Bluetooth.
  async function showBluetooth(box) {
    const r = await call({ cmd: "bt.list" });
    if (!box.isConnected) return;
    if (!r.ok || !r.present) { box.replaceChildren(); return; }
    const msg = el("div", {});
    const err = (e) => msg.replaceChildren(el("div", { class: "msg err" }, t("error", { e })));
    const sw = el("button", { class: "switch" + (r.powered ? " on" : ""), "aria-label": t("bt"), "aria-pressed": r.powered ? "true" : "false",
      onclick: async () => { const s = await call({ cmd: "bt.power", on: !r.powered }); if (!s.ok) err(s.error); showBluetooth(box); } });
    const head = el("div", { class: "row" }, el("h3", { class: "grow" }, t("bt")), sw);
    if (!r.powered) { box.replaceChildren(head, el("div", { class: "muted" }, t("btOff")), msg); return; }
    const act = async (cmd, mac) => {
      msg.replaceChildren(el("div", { class: "muted" }, "…"));
      const s = await call({ cmd, mac });
      if (!s.ok) { err(s.error); return; }
      msg.replaceChildren();
      showBluetooth(box);
    };
    const rows = (r.devices || []).map((d) => el("div", { class: "disk" },
      el("div", { class: "name" }, el("div", {}, d.name), d.connected ? el("div", { class: "muted", style: "font-size:12px" }, t("btConnected")) : null),
      el("button", { onclick: () => act(d.connected ? "bt.disconnect" : "bt.connect", d.mac) }, d.connected ? t("btDisconnect") : t("btConnect")),
      el("button", { onclick: () => act("bt.forget", d.mac) }, t("btForget"))));
    if (!rows.length) rows.push(el("div", { class: "muted" }, t("btNoPaired")));
    const found = el("div", {});
    const find = el("button", { onclick: async () => {
      find.disabled = true;
      found.replaceChildren(el("div", { class: "muted" }, t("btSearching")));
      const s = await call({ cmd: "bt.scan" });
      find.disabled = false;
      if (!s.ok) { found.replaceChildren(); err(s.error); return; }
      const list = (s.devices || []).map((d) => el("div", { class: "disk" }, el("div", { class: "name" }, el("div", {}, d.name)),
        el("button", { class: "primary", onclick: () => pair(d) }, t("btPair"))));
      found.replaceChildren(...(list.length ? list : [el("div", { class: "muted" }, t("btNone"))]));
    } }, t("btFind"));
    const pair = async (d) => {
      const status = el("div", { class: "msg" }, t("btPairing") + " " + d.name);
      found.replaceChildren(status);
      const s = await call({ cmd: "bt.pair", mac: d.mac });
      if (!s.ok) { found.replaceChildren(); err(s.error); return; }
      const started = Date.now();
      const timer = poll(async () => {
        if (!box.isConnected || Date.now() - started > 90000) { clearInterval(timer); return; }
        const p = await call({ cmd: "bt.pairStatus" });
        if (p.passkey && !p.done) status.textContent = t("btPasskey", { k: p.passkey });
        if (!p.done) return;
        clearInterval(timer);
        found.replaceChildren();
        if (p.error) { err(p.error); return; }
        act("bt.connect", d.mac);
      }, 1000);
    };
    box.replaceChildren(head, ...rows, el("div", { class: "row", style: "margin-top:6px" }, find), found, msg);
  }

  // Printers: driverless only (AirPrint / IPP Everywhere), found by
  // themselves; a test page checks one works.
  async function showPrinters(box) {
    const r = await call({ cmd: "printers.list" });
    if (!box.isConnected) return;
    const msg = el("div", {});
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    const list = r.printers || [];
    if (!list.length) { box.replaceChildren(el("div", { class: "muted" }, t("printNone"))); return; }
    const rows = list.map((p) => el("div", { class: "disk" }, el("div", { class: "name" }, el("div", { title: p.name.replace(/_/g, " ") }, p.name.replace(/_/g, " "))),
      el("button", { onclick: async () => {
        const s = await call({ cmd: "printers.test", printer: p.name });
        msg.replaceChildren(el("div", { class: s.ok ? "msg" : "msg err" }, s.ok ? t("printSent", { p: p.name.replace(/_/g, " ") }) : t("error", { e: s.error })));
      } }, t("printTest"))));
    box.replaceChildren(...rows, msg, el("div", { class: "muted", style: "font-size:12px;margin-top:4px" }, t("printHint")));
  }

  function paintMain() {
    const m = main;
    if (m.portable) {
      m.win.textContent = state.window === "fullscreen" ? t("winWindow") : t("winFull");
      if (has("wifi")) paintWifi(m);
      paintSound(m);
      return;
    }
    paintWifi(m);
    paintSound(m);
    m.install.hidden = !state.canInstall;
  }

  function paintWifi(m) {
    let wifiText = t("notConnected");
    if (state.wired) wifiText = t("wired");
    if (state.wifi) wifiText = t("connectedTo", { n: state.wifi.ssid });
    if (state.wifiOn === false) wifiText = t("wifiOff");
    m.wifiText.textContent = wifiText;
    m.sw.className = "switch" + (state.wifiOn ? " on" : "");
    m.sw.setAttribute("aria-pressed", state.wifiOn ? "true" : "false");
    m.nets.hidden = state.wifiOn === false;
    if (state.wifiOn === false) m.list.replaceChildren();
  }

  // Sound, battery and brightness: the same on every edition.
  function paintSound(m) {
    const hasVolume = typeof state.volume === "number";
    m.soundRow.hidden = !hasVolume;
    m.noSound.hidden = hasVolume;
    if (hasVolume && root.activeElement !== m.range) m.range.value = String(state.volume);
    m.mute.textContent = state.muted ? t("unmute") : t("mute");
    m.battery.hidden = !state.battery;
    if (state.battery) {
      m.battery.textContent = t("battery", { p: state.battery.percent }) + (state.battery.charging ? " · " + t("charging") : "");
    }
    m.screenBox.hidden = typeof state.brightness !== "number";
    if (typeof state.brightness === "number" && root.activeElement !== m.light) m.light.value = String(state.brightness);
  }

  // System update: check the latest orOS USB release, download it onto the
  // persistence partition, then a restart starts it.
  async function showUpdate(box) {
    const st = await call({ cmd: "update.status" });
    if (st.ok && st.running) return followUpdate(box);
    if (st.ok && st.progress && st.progress.done && !st.progress.error) {
      box.replaceChildren(el("div", { class: "msg" }, t(portable() ? "appUpdated" : "updated")));
      return;
    }
    box.replaceChildren(el("div", { class: "row" }, el("button", { onclick: () => checkUpdate(box) }, t("updateCheck"))));
    if (st.ok && st.progress && st.progress.error) box.append(el("div", { class: "msg err" }, t("error", { e: st.progress.error })));
  }

  async function checkUpdate(box) {
    box.replaceChildren(el("div", { class: "muted" }, t("searching")));
    const r = await call({ cmd: "update.check" });
    if (!r.ok) { box.replaceChildren(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    // orOS Portable updates orOS itself (kind "app"): staged for the next start.
    const app = r.kind === "app";
    if (app && r.staged) { box.replaceChildren(el("div", { class: "msg" }, t("appUpdated"))); return; }
    if (!r.available) { box.replaceChildren(el("div", { class: "muted" }, t(app ? "appUpdateNone" : "updateNone", { v: r.current || "?" }))); return; }
    box.replaceChildren(el("div", {}, app ? t("appUpdateAvail", { v: r.latest }) : t("updateAvail", { v: r.latest, mb: Math.round(r.size / 1048576) })));
    if (!r.canUpdate) { box.append(el("div", { class: "msg err" }, t("updateNoPersist"))); return; }
    box.append(el("div", { class: "row", style: "margin-top:6px" }, el("button", { class: "primary", onclick: async () => {
      const s = await call({ cmd: "update.start" });
      if (!s.ok) { box.append(el("div", { class: "msg err" }, t("error", { e: s.error }))); return; }
      followUpdate(box);
    } }, t("updateNow"))));
  }

  function followUpdate(box) {
    const msg = el("div", { class: "muted" }, t("updating", { p: 0 }));
    const bar = el("progress", { max: "100", value: "0", style: "width:100%" });
    box.replaceChildren(msg, bar);
    const timer = poll(async () => {
      if (!box.isConnected) { clearInterval(timer); return; }
      const st = await call({ cmd: "update.status" });
      const p = (st.progress) || {};
      if (portable()) {
        // The launcher knows megabytes, not the size: the bar just moves.
        bar.removeAttribute("value");
        msg.textContent = t("appUpdating", { mb: p.mb || 0 });
      } else {
        bar.value = p.percent || 0;
        msg.textContent = t("updating", { p: p.percent || 0 });
      }
      if (!st.running && p.done) {
        clearInterval(timer);
        box.replaceChildren(el("div", { class: p.error ? "msg err" : "msg" }, p.error ? t("error", { e: p.error }) : t(portable() ? "appUpdated" : "updated")));
      }
    }, 1500);
  }

  async function scan(list) {
    list.replaceChildren(el("div", { class: "muted" }, t("searching")));
    const r = await call({ cmd: "wifi.scan" });
    list.replaceChildren();
    if (!r.ok) { list.append(el("div", { class: "msg err" }, t("error", { e: r.error }))); return; }
    if (!r.networks || !r.networks.length) { list.append(el("div", { class: "muted" }, t("none"))); return; }
    r.networks.sort((a, b) => (b.inUse - a.inUse) || (b.signal - a.signal));
    for (const n of r.networks) {
      const bars = n.signal >= 70 ? "▂▄▆█" : n.signal >= 45 ? "▂▄▆" : n.signal >= 20 ? "▂▄" : "▂";
      const item = el("button", { class: "net" + (n.inUse ? " on" : "") },
        el("span", { class: "grow" }, n.ssid), n.secure ? el("span", { html: ICON.lock }) : null, el("span", { class: "muted" }, bars));
      const box = el("div", {});
      item.addEventListener("click", () => {
        if (n.inUse) return;
        if (!n.secure) { connect(n.ssid, "", box); return; }
        const pw = el("input", { type: "password", placeholder: t("password"), autocomplete: "off" });
        const go = el("button", { class: "primary", onclick: () => connect(n.ssid, pw.value, box) }, t("connect"));
        pw.addEventListener("keydown", (e) => { if (e.key === "Enter") go.click(); });
        const eye = el("button", { type: "button", class: "eye", title: t("showPw"), "aria-label": t("showPw"), "aria-pressed": "false", html: ICON.eye });
        eye.addEventListener("click", () => {
          const show = pw.type === "password";
          pw.type = show ? "text" : "password";
          eye.innerHTML = show ? ICON.eyeOff : ICON.eye;
          eye.title = t(show ? "hidePw" : "showPw");
          eye.setAttribute("aria-label", eye.title);
          eye.setAttribute("aria-pressed", String(show));
          pw.focus();
        });
        box.replaceChildren(el("div", { class: "pw" }, pw, eye), go);
        pw.focus();
      });
      list.append(item, box);
    }
  }

  async function connect(ssid, password, box) {
    box.replaceChildren(el("div", { class: "muted" }, t("connecting")));
    const r = await call({ cmd: "wifi.connect", ssid, password });
    box.replaceChildren(el("div", { class: "msg" + (r.ok ? "" : " err") }, r.ok ? t("connected") : t("error", { e: r.error })));
    refresh();
  }

  async function renderInstall() {
    view = "install";
    panel.replaceChildren(el("h2", {}, t("installTitle")), el("p", {}, t("installText")));
    const r = await call({ cmd: "install.disks" });
    if (!r.ok || !r.disks || !r.disks.length) {
      panel.append(el("div", { class: "msg err" }, r.ok ? t("noDisks") : t("error", { e: r.error })),
        el("button", { onclick: renderMain }, t("back")));
      return;
    }
    let chosen = r.disks[0].name;
    for (const d of r.disks) {
      const gb = Math.round(d.size / 1e9);
      const radio = el("input", { type: "radio", name: "disk", value: d.name, checked: d.name === chosen });
      radio.addEventListener("change", () => { chosen = d.name; });
      panel.append(el("label", { class: "chk" }, radio, el("span", {}, (d.model || d.name) + " · " + gb + " GB" + (d.tran ? " · " + d.tran.toUpperCase() : ""))));
    }
    const keep = el("input", { type: "checkbox", checked: true });
    const sure = el("input", { type: "checkbox" });
    const go = el("button", { class: "danger", disabled: true }, t("installNow"));
    sure.addEventListener("change", () => { go.disabled = !sure.checked; });
    go.addEventListener("click", async () => {
      go.disabled = true;
      const s = await call({ cmd: "install.start", disk: chosen, keepData: keep.checked });
      if (!s.ok) { panel.append(el("div", { class: "msg err" }, t("failed", { e: s.error }))); return; }
      renderProgress();
    });
    const zoneBox = el("div", { style: "margin:10px 0" });
    showTime(zoneBox, true);
    panel.append(zoneBox, el("label", { class: "chk" }, keep, el("span", {}, t("keepData"))),
      el("label", { class: "chk" }, sure, el("span", {}, t("understand"))),
      el("div", { class: "row" }, el("button", { onclick: renderMain }, t("back")), el("span", { class: "grow" }), go));
  }

  function renderProgress() {
    view = "installing";
    const fill = el("div", {});
    const msg = el("div", { class: "msg" }, t("installing"));
    panel.replaceChildren(el("h2", {}, t("installTitle")), el("div", { class: "bar" }, fill), msg);
    clearInterval(pollTimer);
    pollTimer = poll(async () => {
      const r = await call({ cmd: "install.status" });
      const p = (r && r.progress) || {};
      fill.style.width = (p.percent || 0) + "%";
      if (!p.done) return;
      clearInterval(pollTimer);
      view = "done";
      if (p.error) {
        msg.className = "msg err";
        msg.textContent = t("failed", { e: p.error });
        panel.append(el("button", { onclick: renderMain }, t("back")));
      } else {
        msg.textContent = t("installed");
        panel.append(el("div", { class: "power" },
          el("button", { class: "primary", html: ICON.power + " " + t("shutdown"), onclick: () => call({ cmd: "power.off" }) })));
      }
    }, 1000);
  }

  // Quick settings: a newer orOS has a GNOME-style panel of its own
  // (qsettings.js, #qs-btn at the right of the bar). Then our System button
  // steps back and the panel gets the computer's parts: volume and
  // brightness sliders, Wi-Fi and Bluetooth buttons and "System" (this
  // file's full panel: printers, disks, time, update, install, report), and
  // its power menu gets Restart the computer and Shut down. orOS sends the
  // DOM event "oros-qs" ("open" / "close"); the elements use its classes.
  // An older orOS without #qs-btn keeps the System button as it was.
  const qsPanel = () => document.getElementById("qs-panel");
  function qsStep() {
    if (btn && document.getElementById("qs-btn")) btn.hidden = true;
  }
  function qsCloseShell() {
    const b = document.getElementById("qs-btn");
    if (b && qsPanel()) b.click();
  }
  function qsPill(icon, title, sub, on, onMain, onMore) {
    const box = el("div", { class: "qs-pill" + (on ? " on" : "") });
    const main = el("button", { type: "button", class: "qs-pill-main", html: icon,
      onclick: (e) => { e.stopPropagation(); onMain(); } });
    if (on !== null) main.setAttribute("aria-pressed", on ? "true" : "false");
    const txt = el("span", { class: "qs-pill-text" }, el("b", {}, title));
    if (sub) txt.append(el("small", {}, sub));
    main.append(txt);
    box.append(main);
    if (onMore) box.append(el("button", { type: "button", class: "qs-pill-more", html: ICON.more,
      "aria-label": title + " · " + t("system"), title: t("system"), onclick: (e) => { e.stopPropagation(); onMore(); } }));
    return box;
  }
  function qsSlider(icon, label, value, min, onButton, onValue, live) {
    const range = el("input", { type: "range", min: String(min), max: "100", "aria-label": label });
    range.value = String(value);
    let busy = false;
    const send = async () => {
      if (busy) return;
      busy = true;
      const v = range.value;
      await onValue(Number(v));
      busy = false;
      if (range.value !== v) send();
    };
    range.addEventListener(live ? "input" : "change", send);
    const b = el("button", { type: "button", html: icon, "aria-label": label, title: label,
      onclick: (e) => { e.stopPropagation(); if (onButton) onButton(); } });
    return el("div", { class: "qs-slider" }, b, range);
  }
  // The full panel, opened from a button in orOS's panel.
  const qsOpenSystem = () => { qsCloseShell(); if (!host) open(); };

  async function qsFill() {
    const sys = document.getElementById("qs-sys");
    const pow = document.getElementById("qs-power-sys");
    if (!sys) return;
    await refresh();
    const bt = has("bt") ? await call({ cmd: "bt.list" }) : null;
    if (!sys.isConnected) return;
    const parts = [];
    if (typeof state.volume === "number" && has("sound")) {
      parts.push(qsSlider(state.muted ? ICON.speakerOff : ICON.speaker, state.muted ? t("unmute") : t("mute"), state.volume, 0,
        async () => { await call({ cmd: "volume.mute", on: !state.muted }); qsFill(); },
        (v) => call({ cmd: "volume.set", value: v }), false));
    }
    if (typeof state.brightness === "number" && has("screen")) {
      parts.push(qsSlider(ICON.sun, t("brightness"), state.brightness, 5, null,
        (v) => call({ cmd: "brightness.set", value: v }), true));
    }
    let wifiSub = t("notConnected");
    if (state.wired) wifiSub = t("wired");
    if (state.wifi) wifiSub = state.wifi.ssid;
    if (state.wifiOn === false) wifiSub = t("wifiOff");
    const grid = el("div", { class: "qs-grid" });
    if (has("wifi")) {
      grid.append(qsPill(state.wired && !state.wifi ? ICON.wired : (state.wifiOn === false ? ICON.wifiOff : ICON.wifi), t("wifi"), wifiSub, state.wifiOn !== false,
        async () => { await call({ cmd: "wifi.radio", on: state.wifiOn === false }); qsFill(); }, qsOpenSystem));
    }
    if (portable() && has("btSettings")) grid.append(qsPill(ICON.bt, t("bt"), t("btSettingsSub"), null, openBtSettings, null));
    if (bt && bt.ok) {
      const conn = (bt.devices || []).filter((d) => d.connected).map((d) => d.name);
      grid.append(qsPill(ICON.bt, t("bt"), bt.powered ? conn.join(", ") : t("btOff"), !!bt.powered,
        async () => { await call({ cmd: "bt.power", on: !bt.powered }); qsFill(); }, qsOpenSystem));
    }
    grid.append(qsPill(ICON.gear, t("system"), t(portable() ? "qsMorePc" : "qsMore"), null, qsOpenSystem, null));
    // orOS puts our buttons first in its own grid (#qs-sys-pills).
    const slot = document.getElementById("qs-sys-pills");
    if (slot) slot.replaceChildren(...grid.childNodes);
    else parts.push(grid);
    sys.replaceChildren(...parts);
    if (pow) {
      const item = (label, cmd) => el("button", { type: "button", class: "qs-menu-item",
        onclick: (e) => { e.stopPropagation(); call({ cmd }); } }, label);
      if (!portable()) pow.replaceChildren(item(t("qsReboot"), "power.reboot"), item(t("shutdown"), "power.off"));
      else {
        // Leaving orOS and the window first; the computer's own power
        // commands ask again.
        const menu = (label, run) => el("button", { type: "button", class: "qs-menu-item",
          onclick: (e) => { e.stopPropagation(); run(); } }, label);
        const items = [];
        if (has("window")) {
          items.push(menu(state.window === "fullscreen" ? t("winWindow") : t("winFull"),
            () => { qsCloseShell(); setWindow(state.window === "fullscreen" ? "window" : "fullscreen"); }));
        }
        if (has("exit")) items.push(menu(t("exit"), exitOros));
        const sure = (label, cmd) => confirmButton(el("button", { type: "button", class: "qs-menu-item" }), label, () => call({ cmd }));
        if (has("power") && has("sleep")) items.push(sure(t("pcSleep"), "power.sleep"));
        if (has("power")) items.push(sure(t("pcRestart"), "power.reboot"), sure(t("pcShutdown"), "power.off"));
        pow.replaceChildren(...items);
      }
    }
  }
  document.addEventListener("oros-qs", (e) => {
    qsStep();
    if (e.detail === "open") qsFill();
  });

  // Tor: the first press asks, then downloads Tor Browser into the home
  // folder (oros-system checks its signature); later presses start it at
  // once. While it downloads, the button shows how far it is.
  function torLabel(text) {
    if (torBtn) torBtn.lastChild.textContent = text;
  }
  async function openTor() {
    if (torTimer) return;
    const st = await call({ cmd: "tor.status" });
    if (st.ok && st.running) return followTor(true);
    if (st.ok && !st.installed) {
      let q = t("torAsk");
      if (st.persistent === false) q = t("torAskNoPersist") + "\n\n" + q;
      if (!confirm(q)) return;
    }
    const r = await call({ cmd: "tor.open" });
    if (!r.ok) alert(t("torError", { e: r.error }));
    else if (r.downloading) followTor(true);
  }
  function followTor(tell) {
    if (torTimer) return;
    torTimer = poll(async () => {
      const st = await call({ cmd: "tor.status" });
      const p = (st.ok && st.progress) || {};
      if (st.ok && st.running) { torLabel(t("torGetting", { p: p.percent || 0 })); return; }
      clearInterval(torTimer);
      torTimer = null;
      torLabel(t("tor"));
      if (tell && p.error) alert(t("torError", { e: p.error }));
    }, 1500);
  }

  function mount() {
    const bar = document.querySelector("#oros-bar .bar-right");
    if (!bar || btn) return !!btn;
    btn = el("button", { type: "button", id: "oros-system-btn", "aria-haspopup": "dialog",
      style: "display:inline-flex;align-items:center;gap:2px;background:none;border:0;color:inherit;font:inherit;padding:4px 6px;cursor:pointer", onclick: (e) => { e.stopPropagation(); open(); } });
    // Internet: a normal browser window over orOS, for the web outside orOS.
    webBtn = el("button", { type: "button", id: "oros-web-btn",
      style: "display:inline-flex;align-items:center;gap:4px;background:none;border:0;color:inherit;font:inherit;padding:4px 6px;cursor:pointer",
      html: ICON.globe + "<span>" + t("web") + "</span>", title: t("webTitle"),
      onclick: (e) => { e.stopPropagation(); call({ cmd: "web.open" }); } });
    // Tor: the official Tor Browser over orOS, downloaded on first use.
    torBtn = el("button", { type: "button", id: "oros-tor-btn",
      style: "display:inline-flex;align-items:center;gap:4px;background:none;border:0;color:inherit;font:inherit;padding:4px 6px;cursor:pointer",
      html: ICON.onion + "<span>" + t("tor") + "</span>", title: t("torTitle"),
      onclick: (e) => { e.stopPropagation(); openTor(); } });
    bar.append(webBtn, torBtn, btn);
    call({ cmd: "tor.status" }).then((st) => { if (st.ok && st.running) followTor(false); });
    applyPlatform();
    paintButton();
    qsStep();
    setTimeout(qsStep, 3000);   // orOS adds #qs-btn at its own boot
    refresh();
    setInterval(refresh, 30000);
    return true;
  }

  // orOS Bridge (plugins and the sound card for Studio) runs here without a
  // window of its own, so when Studio asks to pair, its code shows over orOS.
  // The ping's answer carries the open code (oros-system reads the bridge's
  // --code-file); while one is open it is checked every 1.5 s and the box
  // goes once the code is used. orOS Portable's launcher sends none.
  let codeBox = null, codeTimer = null;
  function bridgeCode(code) {
    code = /^\d{6}$/.test(code || "") ? code : "";
    if (!code) {
      if (codeBox) codeBox.remove();
      codeBox = null;
      clearInterval(codeTimer);
      codeTimer = null;
      return;
    }
    if (!codeBox) {
      codeBox = el("div", { role: "status", "aria-live": "polite" });
      const sh = codeBox.attachShadow({ mode: "open" });
      sh.append(el("style", {}, `
        .box { position: fixed; top: 54px; left: 50%; transform: translateX(-50%); z-index: 2147483001;
          background: #1f1d1a; color: #e8e4dc; border: 1px solid #c8a96e; border-radius: 12px; padding: 12px 18px;
          font: 14px/1.4 system-ui, "Noto Sans", sans-serif; box-shadow: 0 12px 40px rgba(0,0,0,.5); text-align: center; }
        .code { font-size: 34px; font-weight: 700; letter-spacing: .08em; color: #c8a96e; margin-top: 4px; }`),
        el("div", { class: "box" }, el("div", { class: "t" }), el("div", { class: "code" })));
      document.documentElement.append(codeBox);
      codeTimer = poll(async () => {
        const r = await call({ cmd: "bridge.code" });
        if (r.ok) bridgeCode(r.code);
      }, 1500);
    }
    const sh = codeBox.shadowRoot;
    sh.querySelector(".t").textContent = t("bridgeCode");
    sh.querySelector(".code").textContent = code.slice(0, 3) + " " + code.slice(3);
  }

  // Sign of life for oros-session's watchdog: the first ping waits for a
  // painted frame (a window that never paints sends none), then one every
  // 10 s while the page runs. A timer, not frames, keeps it going while the
  // Internet window covers orOS.
  const ping = () => call({ cmd: "ping" }).then((r) => { if (r.ok) bridgeCode(r.bridgeCode); });
  requestAnimationFrame(() => requestAnimationFrame(() => {
    ping();
    setInterval(ping, 10000);
  }));

  const start = () => {
    if (mount()) return;
    const mo = new MutationObserver(() => { if (mount()) mo.disconnect(); });
    mo.observe(document.documentElement, { childList: true, subtree: true });
  };
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", start);
  else start();
})();
