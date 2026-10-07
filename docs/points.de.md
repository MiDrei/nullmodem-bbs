# Points: eine eigene Reader-App (FidoMail und Co.)

[English](points.md) · **Deutsch**

Ein Point ist ein Reader-System unter der Node-Adresse dieser BBS:
`21:3/194.1` ist Point 1 von `21:3/194`. Offline-Reader wie FidoMail auf
iOS arbeiten als Points -- sie rufen die BBS über BinkP an, holen die
abonnierten Areas und geben ab, was darin geschrieben wurde.

## Einrichten

Unter **Admin → FTN → Uplinks (Nodes/Points) → Nodes / Points** einen
Eintrag pro Netzwerk anlegen, an dem der Reader teilnehmen soll:

| Feld | Wert |
| --- | --- |
| Address | die Point-Adresse, z. B. `21:3/194.1` (fsxNet) oder `954:700/14.1` (HobbyNet) |
| Host | eine Bezeichnung (z. B. `fidomail`); der Reader ruft an, er wird nie angerufen. Die Area-Freigaben gelten pro Bezeichnung: eine Bezeichnung für alle Einträge teilt eine Area-Liste, eine pro Eintrag gibt jedem Netzwerk seine eigene |
| Session password | das BinkP-Passwort des Readers -- bei allen Einträgen gleich |
| Areafix password | für die Areafix-Anfragen des Readers (`+AREA`, `%LIST`) |
| Network | das Netzwerk dieser Adresse |
| Downlink | an; dazu **Hold**, weil man einen Reader nicht anrufen kann |

Der Reader wird umgekehrt eingerichtet: diese BBS als Boss-Node (Host
und BinkP-Port, z. B. `bbs.example.com:24554`), seine Point-Adressen,
dasselbe Passwort. Seine Areas abonniert er über Areafix; die im Web-Admin
für ihn angehakten Areas (Area-Freigaben, pro Host-Bezeichnung) sind die,
die er abonnieren darf.

Anders als ein Node bekommt ein Point nur, was ihm gehört:

- Netmail an ihn, nie die von anderen -- die ausgehende Netmail und
  Echomail dieses Systems geht weiterhin an die Hubs;
- die abonnierten Areas: ab zwei Wochen vor dem Abonnieren, jede
  Nachricht einmal (pro Point festgehalten, weil SEEN-BY keine Points
  nennen kann).

Die Logik steckt in `internal/tosser/points.go`.
