# Přenosný deployment balíček

WebUI builderu umí z dokončeného buildu připravit ZIP se skutečně vygenerovanými
Kubernetes manifesty a záznamem vstupů. Balíček lze stáhnout nebo po náhledu uložit
do nakonfigurovaného adresáře nebo Artifactory. Adresář nemusí být Git. Pokud je v Git checkoutu,
add/commit/tag/push řídí jeho vlastník; export je sám neprovádí.

Build, příprava balíčku, uložení výsledku a nasazení jsou samostatné kroky.
Dosavadní **Download YAML ZIP** a kontrolovaná publikace do deployment Gitu se
nemění. Nový balíček se označuje podle existujícího release ID, prostředí a ID
konkrétního buildu. ID buildu je technický identifikátor spuštění, ne další
release ID. Deployment tag `<env>/<release-id>.rN` nadále vzniká v samostatném
Git publikačním kroku; balíček jeho čítač neurčuje.

## WebUI

1. Dokončete build nebo otevřete dokončený výsledek z historie.
2. V části **Deployment package** stiskněte **Prepare package**.
3. Zkontrolujte seznam manifestů, velikost ZIPu, jeho SHA256 a případný cíl.
4. Zvolte **Download package ZIP**, **Save package to directory** nebo **Publish package to Artifactory**.

Příprava zapisuje pouze privátní pracovní kopii balíčku. Do cílového úložiště nic neukládá. Potvrzení ukládá právě zkontrolovaný ZIP, ne nové sestavení nebo
nové čtení původních vstupů. Již připravený balíček lze stáhnout a uložit i po
restartu serveru. Po obnovení stránky lze znovu otevřít build a balíček připravit;
pro tentýž nezměněný build vzniknou stejné bajty a stejný checksum.

## Konfigurace adresáře a konvence cest

Konfigurace `serve --config builder.yaml` může obsahovat `artifact_source`,
`package_output` nebo obě sekce. Stávající `--release-source-config` přijímá stejný
soubor; obě volby najednou se nepoužívají.

```yaml
package_output:
  storage: directory
  directory: ./shared-artifacts
  path: 'packages/{environment}/{release_id}/{build_id}.zip'
```

```sh
kube-build-app serve --root ./environments --releases-root ./releases \
  --work-dir ../builder-work --config ./builder.yaml
```

Relativní `directory` je vůči YAML souboru. Cesta podporuje `{release_id}`,
`{environment}` a `{build_id}` jako jednotlivé segmenty. Výchozí konvence, pokud
`path` chybí, je stejná jako v příkladu. Šablona musí být čistá relativní cesta
končící `.zip`, bez úniku nad kořen adresáře. Neznámé proměnné se odmítají.

Explicitní CLI volby přepisují YAML. Jejich relativní cesty jsou vůči pracovnímu
adresáři:

```sh
kube-build-app serve --root ./environments --releases-root ./releases \
  --package-output-dir ./handoff \
  --package-path 'deliveries/{release_id}/{environment}/{build_id}.zip'
```

`--package-output-dir ''` vypne adresářový export z YAML. Bez exportního cíle
nadále funguje příprava a stažení. Prázdná `--package-path ''` obnoví výchozí
konvenci. Cílový adresář musí být oddělený od vstupů a privátního workdir.
Přesměrování cíle přes symlink po náhledu se odmítne.

## Publikace do Artifactory

```yaml
package_output:
  storage: artifactory
  base_url: https://artifactory.example.invalid/artifactory/deployment-packages
  token_file: ./credentials/package-publisher.token
  overwrite_policy: server-deny
  path: 'packages/{environment}/{release_id}/{build_id}.zip'
```

`base_url` ukazuje přímo na cílový Generic repository. `token_file` a volitelný
`ca_file` se vyhodnocují vůči konfiguračnímu souboru. Účet potřebuje čtení a
vytváření artefaktů v cíli. Artifactory musí tomuto účtu atomicky zakázat
přepsání existujícího artefaktu. `overwrite_policy: server-deny` vyjadřuje tento
kontrakt, jehož dodržování musí zajistit konfigurace serveru.

Po potvrzení builder odešle zkontrolovaný ZIP a stáhne jej zpět pro porovnání.
Shodný existující ZIP znamená bezpečné opakování. Při rozdílném obsahu builder
vrátí konflikt. Pro nasazení stáhněte ZIP a načtěte ho v KDS pod **Deployment
packages**, kde lze ověřit checksum, připravit plán a potvrdit nasazení.

## Obsah ZIPu: první implementovaná verze

```text
package.yaml
build-evidence.json
manifests/
  deployments/…
  services/…
  assets/…
```

`package.yaml` má `apiVersion: kube-build-app/v1`, `kind: DeploymentPackage` a:

- release ID, prostředí, ID buildu a čas jeho dokončení;
- identitu builderu a checksum vstupního release manifestu;
- seznam cest `manifests/…` s SHA256 a počtem bajtů každého souboru;
- cestu a checksum `build-evidence.json`;
- počet privátních vstupů proměnných, jejichž hodnoty se nepřidávají.

`build-evidence.json` obsahuje metadata původního buildu: identity nástrojů,
Git commity a stav vstupních souborů, původ URL/Git releasu a checksumy vstupů
a výstupů. Nekopírují se soubory privátních snapshotů, jejich interní cesty ani
samostatně zachycené hodnoty a hashe vyřešených proměnných. Vykreslené hodnoty
v samotných výsledných manifestech se zachovávají, například v ConfigMap nebo
`env.value`; balení jejich obsah nepozměňuje.

Vnější SHA256 ZIPu je součástí náhledu a hlavičky `X-Checksum-Sha256` při stažení.
Nevkládá se dovnitř vlastního ZIPu. Při předání přes jiné úložiště je třeba zachovat
checksum z důvěryhodného zdroje nebo připnutý Git commit; checksum není podpis.

## Kontroly a uložení

Příprava vyžaduje dokončený build s úplnou evidencí výstupů. Ověřuje přesný seznam,
velikosti a checksumy skutečně čtených souborů. Změna výstupu po buildu vyžaduje
nový build. Již připravený ZIP se při stažení/uložení kontroluje vůči náhledu.
Změna konfigurace cíle po náhledu vyžaduje nový náhled.

Balíček nesmí obsahovat Kubernetes `Secret` ani `SecretList`, také uvnitř List
nebo v dalším YAML dokumentu. Pokud se takový prostředek najde, příprava selže
s názvem souboru, bez zobrazení tajné hodnoty. Prostředek se potichu nevynechává.
Build se zapnutým `--decrypt-secured` se pro tento přenosný balíček odmítá,
protože dešifrované hodnoty mohou být vykreslené i mimo Secret. Nejde o obecnou
detekci tajemství libovolně zapsaných do jiných druhů resources.
Neplatný YAML, nebezpečné cesty a ne-manifestové přípony se odmítají.

Limity: nejvýše 4096 manifestů, 8 MiB na manifest, celkem 64 MiB manifestů,
8 MiB záznamu vstupů a 80 MiB výsledného ZIPu. ZIP obsahuje jen pravidelné soubory,
seřazené a s pevnými ZIP časy. Nebalí zálohu pracovního adresáře.

Export má zámek na cílovou cestu. Kompletní dočasný soubor se atomicky připojí
pod cílovým názvem bez nahrazení existujícího souboru. Vyžaduje souborový systém
s podporou hard linků. Nynější procesové zámky exportu podporují Linux, macOS
a příslušné BSD platformy; příprava a stažení balíčku tento zámek nepoužívají. Existující identický soubor znamená úspěšné opakování;
odlišný obsah je konflikt a zůstane nedotčený. Dočasný soubor se uklidí. Adresáře
a soubor mají privátní oprávnění; jejich zpřístupnění dalším procesům řídí provozovatel.

## Ověření

Testy se syntetickým buildem ověřují inventář, deterministické bajty, kontroly
obsahu, restart, kolize i souběžné uložení do adresáře. Export do Artifactory
ověřuje lokální HTTPS test včetně autentizace, ztracené odpovědi a porovnání
uloženého obsahu. Při publikaci ZIPu do Gitu commit, tag a push vede vlastník
checkoutu.
