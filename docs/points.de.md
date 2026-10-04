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
und BinkP-Port, z. B. `bbs.maik.ch:24554`), seine Point-Adressen,
dasselbe Passwort. Seine Areas abonniert er über Areafix, oder man hakt
sie im Web-Admin an (Area-Freigaben, die pro Host-Bezeichnung gelten).

Anders als ein Node bekommt ein Point nur, was ihm gehört:

- Netmail an ihn, nie die von anderen -- die ausgehende Netmail und
  Echomail dieses Systems geht weiterhin an die Hubs;
- die abonnierten Areas: ab zwei Wochen vor dem Abonnieren, jede
  Nachricht einmal (pro Point festgehalten, weil SEEN-BY keine Points
  nennen kann).

## Als BBS-Benutzer schreiben

**Post as BBS user** macht einen Point zu deinem eigenen Reader für dein
BBS-Konto: Was du im Reader schreibst, erscheint, als hättest du es auf
der BBS selbst geschrieben.

- Echomail wird als Beitrag dieses Benutzers in der Area gespeichert und
  geht mit Adresse, MSGID, Tearline und Origin dieses Systems an den Hub
  -- Kludges, Tearline und Origin des Readers fallen weg. Sie wird nicht
  an den Reader zurückgeschickt.
- Netmail geht als dieser Benutzer von der Adresse dieses Systems in der
  Zone des Empfängers hinaus; Netmail an jemanden auf dieser BBS landet
  in dessen Posteingang, von dir.
- Netmail an diesen Benutzer geht als Kopie auch an den Reader (beim
  Einrichten die letzten zwei Wochen) -- einmal, an seine Point-Adresse
  in der Zone des Absenders (sonst an seine erste), egal welche
  Host-Bezeichnungen. Das Original bleibt im Posteingang der BBS.
- Die im Web-Admin angehakten Areas (Area-Freigaben) sind seine
  Abonnements: keine Areafix-Anfrage nötig. Wegnehmen beendet sie.
  Areafix geht trotzdem auch.

Ein erneut gesendetes Paket (der Reader hat unsere Bestätigung nicht
gesehen) wird nicht doppelt gespeichert: Die MSGID des Readers wird
dafür festgehalten. Der Benutzer muss existieren; wird er umbenannt oder
gelöscht, schlagen die Sitzungen des Readers fehl, bis die Einstellung
stimmt -- seine Post wird nie stattdessen unter der Point-Adresse
gespeichert.

Die Logik steckt in `internal/tosser/points.go`.
