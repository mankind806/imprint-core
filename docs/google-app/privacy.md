# Privacy Policy for imprint-sync (`imprint-sync-lesen`)

| Aspect | Policy |
|---|---|
| Effective Date | 2026-10-01 |
| Application | `imprint-sync-lesen` (Google Cloud project `imprint-sync`) |
| Scopes Used | `gmail.readonly`, `contacts.readonly`, `calendar.readonly` |
| Data Destination | Local computer only (no external servers or cloud services) |
| Token Security | Locally encrypted token storage |
| Third-Party Sharing | No sale, no analytics, no advertising; only owner-directed AI processing (see below) |
| Human Access | None (no human reads or reviews user data) |
| Revocation | Via Google Account settings at `https://myaccount.google.com/permissions` |
| Contact | Open an issue in this repository |

This privacy policy applies to the desktop OAuth client application `imprint-sync-lesen` (registered under the Google Cloud project `imprint-sync`).

## Data Accessed

The application requests only read-only access to the owner's own Google account data through the following OAuth scopes:
- `https://www.googleapis.com/auth/gmail.readonly`: Read-only access to email messages and metadata for local archival.
- `https://www.googleapis.com/auth/contacts.readonly`: Read-only access to contacts for local archival.
- `https://www.googleapis.com/auth/calendar.readonly`: Read-only access to calendars and events for local archival.

No write, modify, or delete permissions are requested.

## Data Storage and Destination

All data fetched via Google APIs is stored exclusively on the owner's local computer.
- **No external servers:** The application communicates directly between the local computer and official Google API endpoints. There are no intermediary servers, cloud databases, or backend services.
- **Encrypted credentials:** OAuth tokens and refresh credentials are stored encrypted on the local machine.
- **No sale, no sharing:** Data is never sold, shared with, or disclosed to third parties for their own purposes.
- **Owner-directed AI processing:** When the owner asks their own AI assistant (currently Claude Code by Anthropic) a question, selected excerpts may be sent to that assistant to answer the owner's question. This happens only at the owner's direction, for the owner's own use. The application itself does not use Google user data to train generalized AI models; the owner is responsible for the training settings of the AI provider they choose.
- **No analytics or tracking:** The application contains no telemetry, analytics, tracking, or advertising libraries.
- **No human access besides the owner:** Apart from the owner reading their own data, no humans, including developers or contributors, read or review data retrieved from Google APIs.

## Retention and Deletion

Data retention is under the sole control of the local device owner:
- **Local deletion:** The owner may delete locally archived data and tokens at any time directly on their file system.
- **Revoking access:** Access can be revoked at any time through the Google Account settings at [myaccount.google.com/permissions](https://myaccount.google.com/permissions). Upon revocation, the application can no longer access Google APIs.

## Google API Services User Data Policy (Limited Use)

`imprint`'s use and transfer to any other app of information received from Google APIs will adhere to the [Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy), including the Limited Use requirements.

## Changes to this Policy

Any changes to this policy will be committed directly to this document within this repository.

## Contact

For questions or concerns, please open an issue in this repository on GitHub. No email address is used for contact.

---

## Deutsche Fassung: Datenschutzerklärung

| Aspekt | Richtlinie |
|---|---|
| Stand | 2026-10-01 |
| Anwendung | `imprint-sync-lesen` (Google-Cloud-Projekt `imprint-sync`) |
| Genutzte Scopes | `gmail.readonly`, `contacts.readonly`, `calendar.readonly` |
| Datenziel | Ausschließlich der lokale Rechner (keine externen Server, keine Cloud) |
| Token-Sicherheit | Verschlüsselte lokale Speicherung von Tokens |
| Weitergabe an Dritte | Kein Verkauf, keine Analytik, keine Werbung; nur KI-Verarbeitung im Auftrag des Besitzers (siehe unten) |
| Menschlicher Zugriff | Keiner (keine Personen lesen oder prüfen Nutzerdaten) |
| Widerruf | Über die Google-Kontoeinstellungen unter `https://myaccount.google.com/permissions` |
| Kontakt | Ein Issue in diesem Repository eröffnen |

Diese Datenschutzerklärung gilt für die Desktop-OAuth-Client-Anwendung `imprint-sync-lesen` (registriert im Google-Cloud-Projekt `imprint-sync`).

### Zugriffene Daten

Die Anwendung fordert ausschließlich Nur-Lese-Zugriff auf das eigene Google-Konto des Inhabers über die folgenden OAuth-Scopes an:
- `https://www.googleapis.com/auth/gmail.readonly`: Nur-Lese-Zugriff auf E-Mail-Nachrichten und Metadaten zur lokalen Archivierung.
- `https://www.googleapis.com/auth/contacts.readonly`: Nur-Lese-Zugriff auf Kontakte zur lokalen Archivierung.
- `https://www.googleapis.com/auth/calendar.readonly`: Nur-Lese-Zugriff auf Kalender und Termine zur lokalen Archivierung.

Es werden keine Schreib-, Änderungs- oder Löschberechtigungen angefordert.

### Datenspeicherung und Datenziel

Alle über Google-APIs abgerufenen Daten werden ausschließlich auf dem lokalen Rechner des Inhabers gespeichert.
- **Keine externen Server:** Die Anwendung kommuniziert ausschließlich direkt zwischen dem lokalen Rechner und den offiziellen Google-API-Endpunkten. Es existieren keine Zwischenserver, Cloud-Datenbanken oder Backend-Dienste.
- **Verschlüsselte Zugangsdaten:** OAuth-Tokens und Anmeldedaten werden auf dem lokalen System verschlüsselt abgelegt.
- **Kein Verkauf, keine Weitergabe:** Daten werden nie verkauft und nie an Dritte für deren eigene Zwecke weitergegeben.
- **KI-Verarbeitung im Auftrag des Besitzers:** Stellt der Besitzer seinem eigenen KI-Assistenten (derzeit Claude Code von Anthropic) eine Frage, können ausgewählte Ausschnitte an diesen Assistenten gehen, um die Frage zu beantworten. Das geschieht nur auf Veranlassung des Besitzers und für dessen eigene Nutzung. Die Anwendung selbst nutzt Google-Nutzerdaten nicht zum Training allgemeiner KI-Modelle; für die Trainingseinstellungen des gewählten KI-Anbieters ist der Besitzer verantwortlich.
- **Keine Analysedienste oder Tracking:** Die Anwendung enthält keine Telemetrie, Analysewerkzeuge, Tracking- oder Werbedienste.
- **Kein menschlicher Zugriff außer dem Besitzer:** Außer dem Besitzer, der seine eigenen Daten liest, lesen oder prüfen keine Personen, auch keine Entwickler oder Mitwirkenden, Daten, die über Google-APIs empfangen wurden.

### Speicherdauer und Löschung

Die Aufbewahrungsdauer liegt vollständig in der Kontrolle des lokalen Rechnerinhabers:
- **Lokale Löschung:** Der Inhaber kann lokal archivierte Daten und Anmelde-Tokens jederzeit direkt im Dateisystem löschen.
- **Widerruf des Zugriffs:** Der Zugriff kann jederzeit über die Google-Kontoeinstellungen unter [myaccount.google.com/permissions](https://myaccount.google.com/permissions) widerrufen werden. Nach einem Widerruf hat die Anwendung keinen Zugriff mehr auf die Google-APIs.

### Einhaltung der Google API Services User Data Policy (Limited Use)

Die Nutzung und Übertragung von Informationen, die über Google-APIs empfangen wurden, durch `imprint` an andere Anwendungen entspricht der [Google API Services User Data Policy](https://developers.google.com/terms/api-services-user-data-policy), einschließlich der Bestimmungen zur beschränkten Nutzung (Limited Use requirements).

### Änderungen dieser Erklärung

Änderungen an dieser Datenschutzerklärung werden direkt in dieser Datei in diesem Repository festgeschrieben.

### Kontakt

Bei Fragen oder Anliegen bitte ein Issue in diesem GitHub-Repository eröffnen. Für Kontaktaufnahmen wird keine E-Mail-Adresse verwendet.
