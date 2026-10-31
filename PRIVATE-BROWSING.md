# Private browsing on orOS USB

**EN** · [ΕΛ](#ιδιωτική-περιήγηση-στο-oros-usb)

Your internet provider can see which sites you visit. A **VPN browser extension** hides that for one browser window only, while orOS itself (sync, Mail and the other apps) keeps using your normal connection. orOS does not include or recommend a VPN provider; pick one yourself.

## Where it goes

Put the extension in the **Internet** window (System › Internet). It has its own profile in `~/oros/data/web`, separate from the orOS window, which has no toolbar for extensions.

The extension and its sign-in are kept only when the stick has a persistence partition (see "Using it" in the [README](README.md)) or orOS is installed on the computer. Without one they are gone at every shutdown. A System update keeps them, because it does not touch the home folder.

## Steps

1. Open **System › Internet**.
2. Open the browser's extension store and install the extension of the VPN provider you chose. Check the publisher name, because copies with similar names exist.
3. Pin it: the puzzle icon at the top right › pin.
4. Sign in or create an account with the provider, then connect.
5. Check it: open a DNS leak test page. It should show the provider's servers, not your internet provider.
6. Close and reopen the Internet window. The extension should still be there; if not, the stick has no persistence.

## Choosing a provider

- A free plan without a data cap, or with one large enough for what you do.
- Its no-logs claim checked by an independent audit, and a privacy policy you can read.
- Open-source apps and extensions are a plus.
- Avoid free VPNs from unknown companies: many sell your browsing data.

## Good to know

- Only the window with the extension is covered. Streams opened inside orOS (Television, Radio) use your normal connection.
- A browser extension is a proxy, not a full VPN. Before you press Connect, or if the connection drops, the window uses your normal connection. Check the icon first.
- Anyone who finds the stick can see the Internet window's history and sign-ins: the persistence partition is not encrypted (orOS's own vault is).
- For really sensitive searches, Tor Browser (torproject.org, free and open source) hides more than a VPN, but it is too slow for video. Your internet provider can see that you use Tor.
- A private (incognito) window hides nothing from your internet provider; it only keeps no history on the stick.

---

# Ιδιωτική περιήγηση στο orOS USB

[EN](#private-browsing-on-oros-usb) · **ΕΛ**

Ο πάροχος internet σου βλέπει ποιες σελίδες επισκέπτεσαι. Ένα **VPN extension για τον browser** το κρύβει μόνο για ένα παράθυρο, ενώ το ίδιο το orOS (sync, Mail και οι άλλες εφαρμογές) συνεχίζει με την κανονική σου σύνδεση. Το orOS δεν περιέχει ούτε προτείνει πάροχο VPN· διαλέγεις εσύ.

## Πού μπαίνει

Βάλε το extension στο παράθυρο **Internet** (System › Internet). Έχει δικό του προφίλ στο `~/oros/data/web`, χωριστά από το παράθυρο του orOS, που δεν έχει γραμμή εργαλείων για extensions.

Το extension και η σύνδεσή σου σε αυτό μένουν μόνο αν το stick έχει persistence partition (δες «Using it» στο [README](README.md)) ή αν το orOS είναι εγκατεστημένο στον υπολογιστή. Χωρίς αυτό χάνονται σε κάθε κλείσιμο. Το System update τα κρατά, γιατί δεν αγγίζει τον φάκελο του χρήστη.

## Βήματα

1. Άνοιξε **System › Internet**.
2. Άνοιξε το κατάστημα extensions του browser και εγκατάστησε το extension του παρόχου VPN που διάλεξες. Έλεγξε το όνομα του εκδότη, γιατί υπάρχουν αντίγραφα με παρόμοια ονόματα.
3. Καρφίτσωσέ το: εικονίδιο παζλ πάνω δεξιά › καρφίτσωμα.
4. Συνδέσου ή φτιάξε λογαριασμό στον πάροχο και πάτα σύνδεση.
5. Έλεγξέ το: άνοιξε μια σελίδα DNS leak test. Πρέπει να δείχνει τους servers του παρόχου, όχι τον πάροχο internet σου.
6. Κλείσε και ξανάνοιξε το παράθυρο Internet. Το extension πρέπει να είναι ακόμα εκεί· αν όχι, το stick δεν έχει persistence.

## Πώς διαλέγεις πάροχο

- Δωρεάν πρόγραμμα χωρίς όριο δεδομένων, ή με όριο που σου φτάνει.
- Η δήλωση «no logs» ελεγμένη από ανεξάρτητο audit, και πολιτική απορρήτου που διαβάζεται.
- Ανοιχτός κώδικας στις εφαρμογές και στο extension είναι συν.
- Απόφυγε δωρεάν VPN άγνωστων εταιρειών: πολλά πουλάνε τα δεδομένα περιήγησης.

## Καλό να ξέρεις

- Καλύπτεται μόνο το παράθυρο με το extension. Ό,τι ανοίγει μέσα στο orOS (Television, Radio) περνά από την κανονική σου σύνδεση.
- Ένα extension είναι proxy, όχι πλήρες VPN. Πριν πατήσεις σύνδεση, ή αν πέσει η σύνδεση, το παράθυρο βγαίνει από την κανονική σου σύνδεση. Κοίτα πρώτα το εικονίδιο.
- Όποιος βρει το stick βλέπει το ιστορικό και τις συνδέσεις του παραθύρου Internet: το persistence partition δεν είναι κρυπτογραφημένο (το vault του orOS είναι).
- Για πραγματικά ευαίσθητες αναζητήσεις, το Tor Browser (torproject.org, δωρεάν και ανοιχτού κώδικα) κρύβει περισσότερα από ένα VPN, αλλά είναι πολύ αργό για βίντεο. Ο πάροχος internet βλέπει ότι χρησιμοποιείς Tor.
- Ένα ιδιωτικό (incognito) παράθυρο δεν κρύβει τίποτα από τον πάροχο internet· απλώς δεν κρατά ιστορικό στο stick.
