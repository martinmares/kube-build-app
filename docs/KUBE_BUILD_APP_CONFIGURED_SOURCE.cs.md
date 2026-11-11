# Konfigurovaný zdroj release manifestů

Builder může ve WebUI načíst ImageRelease z předem nastaveného HTTPS úložiště,
například Artifactory. Správce určí adresu, konvenci cesty a soubor s přístupovým
tokenem. Uživatel vyplní release ID a zkontroluje stažený manifest před buildem.
Čtení nemění schéma dokumentu a neprovádí převod dodavatelského BOM.

## Konfigurace serveru

Samostatný soubor `release-source.yaml`:

```yaml
artifact_source:
  storage: artifactory
  base_url: https://artifacts.example.invalid/artifactory/releases
  path: 'release/{release_id}/release.yaml'
  token_file: ./credentials/artifactory.token  # optional bearer token
  ca_file: ./certificates/company-ca.pem      # optional additional trusted CA
  # sha256: '<64 hex characters>'             # optional fixed document checksum
```

```sh
kube-build-app serve --root ./environments --releases-root ./releases \
  --work-dir ../builder-work --release-source-config ./release-source.yaml
```

Soubor obsahuje pouze sekci `artifact_source`. Neznámá pole a více YAML dokumentů
se odmítají. Relativní `token_file` a `ca_file` jsou vůči tomuto konfiguračnímu
souboru. Token se čte až při vyžádaném stažení; není vložen do URL, odpovědi
katalogu ani uloženého buildu. Start serveru a zobrazení stránky zdroj nestahují.

Cesta podporuje `{release_id}`, `{environment}`, `{revision}` a `{distribution}`.
Hodnoty jsou jednotlivé segmenty složené z písmen ASCII, číslic, teček, pomlček
nebo podtržítek a začínají písmenem či číslicí. Neumožňují lomítka nebo `..`.
Prostředí se přebírá z výběru prostředí buildu. Revision a distribution se zobrazí
jen tehdy, pokud je šablona potřebuje. Revision zde slouží pouze k výběru cesty
vstupu; neřídí čítač deployment tagu. Release ID uvnitř dokumentu musí souhlasit
se zadaným ID i u cesty bez proměnné `{release_id}`.

Očekávaný SHA256 je pro tento režim povinný. Pokud jej správce neuvede v YAML,
uživatel jej zadá ve formuláři pro konkrétní dokument. Pevný checksum z konfigurace
nelze formulářem přepsat. Získávejte jej z důvěryhodného vstupu; samotný checksum
není digitální podpis.

## Přepsání přes CLI

Následující volby `serve` přepisují odpovídající YAML pole:

| CLI | YAML |
| --- | --- |
| `--release-base-url` | `base_url` |
| `--release-path` | `path` |
| `--release-sha256` | `sha256` |
| `--release-token-file` | `token_file` |
| `--release-source-ca-file` | `ca_file` |

Explicitní prázdná hodnota pole vymaže, například `--release-token-file ''`
vypne nakonfigurovaný token a `--release-sha256 ''` vyžádá checksum ve formuláři.
Relativní CLI cesty jsou vůči pracovnímu adresáři. Bez YAML lze zadat potřebné
hodnoty přímo:

```sh
kube-build-app serve --root ./environments --releases-root ./releases \
  --release-base-url https://artifacts.example.invalid/artifactory/releases \
  --release-path 'release/{release_id}/release.yaml' \
  --release-token-file ./credentials/artifactory.token
```

`--release-ca-file` nadále platí pouze pro jednorázovou volbu **Read from URL**.
Ta nepřebírá přístupy ani CA konfigurovaného zdroje. Pro konfigurovaný zdroj
použijte `ca_file` nebo `--release-source-ca-file`.

## Práce ve WebUI a uchování vstupu

1. Vyberte prostředí a **Configured Artifactory**.
2. Zadejte release ID, případně další proměnné cesty a očekávaný checksum.
3. **Read and review manifest** stáhne a ověří dokument; vpravo zobrazí YAML,
   ID, počet images, výslednou URL a checksum.
4. **Build manifests** pracuje s uchovanými bajty, bez opakovaného stažení.

Změna formuláře nebo prostředí zruší předchozí výběr. Chybné načtení nezanechá
starý manifest připravený k buildu. Platí stejné ukládání dokumentu a záznamu
vstupů jako u [jednorázové URL](KUBE_BUILD_APP_URL_INPUT.cs.md): privátní snapshot,
URL a checksum bez credentials, kontrola změny obsahu, dokončení z uchovaného
výběru i po restartu serveru. Obnovení stránky neobnovuje dosud nesestavený výběr.

TLS se ověřuje, přesměrování se nenásleduje. Neplatný ImageRelease, HTML stránka,
nesouhlasící checksum nebo release ID se odmítají. Limit dokumentu je 8 MiB.
Publikace deployment Gitu i nasazení jsou nadále samostatné kroky.

## Ověřený rozsah a další práce

Implementováno je konfigurované HTTPS čtení ve WebUI a jeho nastavení přes YAML
nebo argumenty `serve`. Přímý build z konfigurovaného zdroje bez WebUI zatím není
implementovaný. Lokální `--releases-root` zůstává povinný; může být prázdný a nemusí
být Git. Konfigurovaný Git zdroj s výběrem tagu/commitu, spravovaná Git kopie a
publikace deployment balíčku do Artifactory zbývají.

Ověřeno syntetickým lokálním HTTPS serverem s vlastním CA a tokenem, testy shody
ID/checksumu, izolace credentials, CLI precedence, restartu a sestavení po výpadku
zdroje. Konkrétní instalace JFrog nebyla integračně testována.
