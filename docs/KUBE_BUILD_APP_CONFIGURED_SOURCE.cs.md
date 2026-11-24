# Konfigurovaný zdroj release manifestů

Builder může ve WebUI načíst ImageRelease z předem nastaveného HTTPS úložiště
(například Artifactory) nebo z Git tagu v připojeném checkoutu či ve vlastní kopii
vzdáleného zdroje. Správce určí adresu, konvenci cesty a soubor s přístupovým
tokenem. Uživatel vyplní release ID a zkontroluje stažený manifest před buildem.
Čtení nemění schéma dokumentu a neprovádí převod dodavatelského BOM.

## Konfigurace serveru

Samostatný soubor `release-source.yaml` pro HTTPS:

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

Soubor může obsahovat `artifact_source` a/nebo `package_output` pro
[přenosný deployment balíček](KUBE_BUILD_APP_PACKAGE.cs.md). Neznámá pole a více YAML dokumentů
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

Očekávaný SHA256 je pro HTTPS režim povinný. Pokud jej správce neuvede v YAML,
uživatel jej zadá ve formuláři pro konkrétní dokument. Pevný checksum z konfigurace
nelze formulářem přepsat. Získávejte jej z důvěryhodného vstupu; samotný checksum
není digitální podpis.

## Přepsání přes CLI

Následující volby `serve` přepisují odpovídající YAML pole:

| CLI | YAML |
| --- | --- |
| `--release-storage` | `storage` |
| `--release-repository` | `repository` |
| `--release-tag` | `tag` |
| `--release-git-url` | `remote.url` |
| `--release-git-username` | `remote.username` |
| `--release-git-password-file` | `remote.password_file` |
| `--release-git-ca-file` | `remote.ca_file` |
| `--release-git-refresh-interval` | `remote.refresh_interval` |
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

1. Vyberte prostředí a **Configured Artifactory** nebo **Configured Git**.
2. Zadejte release ID, případně další proměnné cesty a očekávaný checksum.
3. **Read and review manifest** stáhne a ověří dokument; vpravo zobrazí YAML,
   ID, počet images, zdrojovou URL nebo Git tag/commit/cestu a checksum.
4. **Build manifests** pracuje s uchovanými bajty, bez opakovaného stažení.

Změna formuláře nebo prostředí zruší předchozí výběr. Chybné načtení nezanechá
starý manifest připravený k buildu. Platí stejné ukládání dokumentu a záznamu
vstupů jako u [jednorázové URL](KUBE_BUILD_APP_URL_INPUT.cs.md): privátní snapshot,
URL a checksum bez credentials, kontrola změny obsahu, dokončení z uchovaného
výběru i po restartu serveru. Obnovení stránky neobnovuje dosud nesestavený výběr.

TLS se ověřuje, přesměrování se nenásleduje. Neplatný ImageRelease, HTML stránka,
nesouhlasící checksum nebo release ID se odmítají. Limit dokumentu je 8 MiB.
Publikace deployment Gitu i nasazení jsou nadále samostatné kroky.

## Ověření

WebUI i CLI umějí načíst release z konfigurovaného HTTPS zdroje, lokálního Git
tagu nebo spravované vzdálené Git kopie. `--releases-root` přidává procházení
lokálních souborů ve WebUI. Bez něj se otevře konfigurovaný zdroj, případně
jednorázové načtení z URL. Výstup lze předat jako
[deployment ZIP](KUBE_BUILD_APP_PACKAGE.cs.md), včetně publikace do Artifactory.

Ověřeno syntetickým lokálním HTTPS serverem s vlastním CA a tokenem, testy shody
ID/checksumu, izolace credentials, CLI precedence, restartu a sestavení po výpadku
zdroje. Konkrétní instalace JFrog nebyla integračně testována.

## Git: existující lokální checkout a tag

```yaml
artifact_source:
  storage: git
  repository: ./image-releases
  tag: 'release/{release_id}'
  path: 'releases/{release_id}.yaml'
  # sha256: '<optional fixed expected checksum>'
```

`repository` musí být kořen existujícího checkoutu. Relativní cesta v YAML je
vůči konfiguraci; CLI `--release-repository` je vůči pracovnímu adresáři.
Git varianta nepřijímá `base_url`, `token_file` ani `ca_file`. Server ji lze
nastavit i bez YAML pomocí `--release-storage git --release-repository …
--release-tag 'release/{release_id}' --release-path 'releases/{release_id}.yaml'`.

Šablona tagu podporuje stejné proměnné jako cesta. Při požadavku server jednou
určí commit z přesné reference `refs/tags/<tag>`. Lehké i anotované tagy jsou
podporované. Stejnojmenná větev tag nenahrazuje. Dokument pak načte přímo z Git
objektu daného commitu. Necommitnuté pracovní soubory ani aktuální HEAD výběr
neovlivňují. Čtení nepoužívá Git replace refs, filtry ani checkout souborů.

Náhled a evidence uvádějí repository, tag, celý resolved commit, cestu a SHA256
skutečného dokumentu. Checksum z konfigurace je volitelný; pokud je nastaven,
musí souhlasit. Formulář Git režimu nežádá povinný checksum. Původ je součástí
uchovaného výběru, buildu, evidence vstupů i následné lokální Git publikace.
Nejde o commit soukromého pracovního adresáře builderu ani o aktuální HEAD zdroje.

Další načtení pozoruje případný posun tagu. Již načtený a zkontrolovaný výběr
zůstává připnutý na původní obsah: build jej po restartu použije i bez původního
repository. Chybějící nebo smazaný tag při novém čtení způsobí chybu, ne fallback
na pracovní soubor nebo dřívější výběr. Symlinky, adresáře a soubory nad 8 MiB
se odmítají. Změna privátně uchovaného dokumentu se rovněž odmítne.

Zdroj se nefetcheuje, nestaguje, necommituje a nepushuje. Synchronizaci tohoto
připojeného checkoutu řídí jeho vlastník. Pracovní adresář builderu a checkout pro
publikaci musejí být oddělené od zdrojového Git repozitáře. Pro vzdálené URL použijte následující variantu `remote`.

Ověření používá skutečné lokální Git repozitáře se syntetickými manifesty:
lehké/anotované a posunuté/smazané tagy, změněný working tree, restart a build
bez zdrojového repozitáře, neplatný checksum, symlink, velikost a zásah do snapshotu.

## Git: vlastní čtecí kopie vzdáleného zdroje

```yaml
artifact_source:
  storage: git
  tag: 'release/{release_id}'
  path: 'releases/{release_id}.yaml'
  remote:
    url: https://git.example.invalid/team/image-releases.git
    username: release-reader
    password_file: ./credentials/git.token
    ca_file: ./certificates/company-ca.pem  # optional
    refresh_interval: 5m                  # optional
```

`repository` a `remote` se vzájemně vylučují. Vzdálený zdroj může používat HTTPS,
`ssh://git@host/path` nebo `file:///absolute/path` pro místní předávání/testy.
Heslo nesmí být v URL; query a fragment se nepřijímají. HTTPS username a
password_file se zadávají společně. Token lze předat jako heslo se jménem účtu
požadovaným Git serverem. Relativní password_file/ca_file v YAML jsou vůči
konfiguraci, v CLI vůči pracovnímu adresáři. Soubor obsahuje jeden neprázdný řádek;
okolní whitespace se odstraní. Bez explicitních HTTPS údajů může Git použít
předem nastavený credential helper, bez interaktivního dotazování.

SSH používá připravený klíč/agenta a ověřený známý host. Nepřijímá zde HTTPS
password_file ani ca_file. Host key se musí připravit předem; program nevypíná
jeho ověření a neotevírá přihlašovací dialog. Živý SSH server zatím integračně
ověřený není. HTTPS certifikát se ověřuje, redirecty se nenásledují.

Server vytvoří pod privátním workdir vlastní bare Git kopii bez pracovních souborů.
Kontroluje její vlastnictví, odmítá cizí adresáře, symlinky a běžné checkouty.
Zámek chrání refresh i následné čtení mezi požadavky a mezi procesy. Současná
implementace těchto zámků podporuje Linux, macOS a příslušné BSD platformy;
na nepodporované platformě se spravované čtení výslovně odmítne.

Každé **Read and review manifest** nejprve provede nový fetch tagů. Aktualizace
lokálních refs je atomická a odráží i posunuté/smazané tagy. Chyba sítě nebo
přístupů nepřejde na starý obsah kopie. Po úspěšném fetch se tag přeloží na
jediný commit a čte se jeho blob stejně jako u připojeného checkoutu.
Už zkontrolovaný snapshot naopak dál funguje i po výpadku zdroje nebo restartu.

`refresh_interval` zapíná navíc okamžitý a potom periodický fetch v procesu
`serve`. Minimum je `1s`; bez této volby se stahuje pouze při explicitním čtení.
Plánovač se zastaví při ukončení serveru. Zobrazení stránky samo fetch nevyvolává.
WebUI průběžně ukazuje poslední úspěšný refresh a chybu. Chyba nezmaže čas
předchozího úspěchu a není prezentována jako aktuálně úspěšné stažení.

Evidence obsahuje původní URL, tag, celý commit, cestu a checksum, nikoliv interní
cestu čtecí kopie. Přihlašovací hodnoty jsou pouze v prostředí příslušného Git
procesu: nejsou v argumentech, Git configu, uloženém stavu ani evidenci. S explicitním
účtem se nepoužije jiný credential helper jako fallback. Nastavení Git trace se
nepřebírá z prostředí, aby se přístupy nedostaly do debug výstupu.

CLI příklad nastavení serveru:

```sh
kube-build-app serve --root ./environments --releases-root ./releases \
  --release-storage git \
  --release-git-url https://git.example.invalid/team/image-releases.git \
  --release-git-username release-reader \
  --release-git-password-file ./credentials/git.token \
  --release-git-refresh-interval 5m \
  --release-tag 'release/{release_id}' --release-path 'releases/{release_id}.yaml'
```

`--release-git-refresh-interval ''` vypne naplánovaný fetch z YAML. Ruční čtení
nadále fetchuje. `--release-git-url '' --release-repository ./checkout` přepne
Git konfiguraci na připojený lokální checkout; celý blok remote se vynechá.
Při přechodu opačným směrem použijte `--release-repository ''` spolu se vzdáleným URL.
Nejde o bezobslužný CLI build; volby zde nastavují server pro WebUI.

Ověřeno se skutečným Gitem proti lokálnímu file:// a HTTPS serveru s Basic přístupy
a vlastním CA: změny/smazání tagů, nedostupný zdroj bez stale fallbacku, restart
a offline build, vlastnictví kopie, zámky, periodický fetch a jeho cancellation,
neuložení hesla/headeru, odmítnutí nedůvěryhodného TLS a redirectu. Živá firemní
Git/SSO instalace nebyla integračně testována.


## Přímé načtení při CLI buildu

```sh
kube-build-app build -e dev -R environments \
  --release-source-config ./builder.yaml --release-id SWD-42 \
  --release-source-sha256 <sha256> --evidence-dir ./build-evidence-SWD-42 \
  --image-policy strict --output-dir ./deploy/dev
```

`build` čte stejný blok `artifact_source` jako WebUI. Pro Git použije konkrétní
commit pod vybraným tagem. Pro Artifactory ověří očekávaný checksum z konfigurace
nebo argumentu. `--release-source-revision` a `--release-source-distribution`
vyplní případné proměnné v cestě nebo tagu. Pokud zapnete evidenci, obsahuje
přesný původ a checksum. `--release-manifest` zůstává samostatný vstup pro
lokální soubory.
