# Jednorázový release manifest z URL

Samostatné webUI `kube-build-app` umí vedle místních souborů načíst nativní
ImageRelease z přímého HTTPS odkazu. Používá stejnou validaci a renderer jako
dosavadní výběr souboru a CLI. Nejde o import dodavatelského BOM: převod BOM
do image releasu zůstává úkolem kube-release.

## Použití

1. V Build inputs vyberte prostředí a Release source → Read from URL.
2. Zadejte přímé URL. Volitelně nastavte Bearer token nebo Basic jméno/heslo
   a nezávisle získaný očekávaný SHA256.
3. Read and review manifest načte a ověří dokument. Pravý panel zobrazí přesný
   stažený YAML, Release ID, počet images, adresu a checksum.
4. Build manifests použije tuto uloženou kopii, bez dalšího síťového načtení.
   Následuje dosavadní log, resource summary, soubory a samostatná publikace.

Při změně URL, přihlášení nebo očekávaného checksumu se předchozí výběr zruší.
Po neúspěšném načtení nelze sestavit starý manifest jako nový výsledek čtení.
Bez očekávaného SHA256 se checksum pouze vypočte; UI jej neoznačuje za nezávisle
ověřený. Při dodaném checksumu musí přesně odpovídat staženému obsahu.

Současný `serve` stále vyžaduje `--releases-root` pro místní katalog. Může to být
obyčejný adresář bez Gitu, i prázdný. Výběr z URL jej nepřepisuje.

```sh
kube-build-app serve \
  --root ./environments \
  --releases-root ./image-releases \
  --work-dir ./.tmp/build-web \
  --release-ca-file ./certificates/company-ca.pem
```

`--release-ca-file` je volitelná dodatečná CA pro HTTPS. Výchozí je systémová
důvěra; TLS ověřování se nevypíná. Cesta je vůči adresáři spuštění příkazu.
URL musí být přímé HTTPS bez userinfo, query a fragmentu. Přesměrování, HTML
přihlašovací stránka, nesprávný formát a soubory nad 8 MiB se odmítají.

## Uložená data a původ

Stažený manifest a bezpečná metadata se uloží privátně pod
`<work-dir>/.url-releases/<id>/`. Po restartu lze dříve stažený výběr použít;
před sestavením se znovu ověří jeho místní obsah a metadata. Samotné pokračování
nevydává další síťový požadavek. Výběr zobrazený pouze v paměti prohlížeče se
po úplném reloadu stránky automaticky neobnoví; hotové buildy zůstávají v historii.

Každý build má vlastní `release.yaml` a `build.json`, obsahující `release_source`:
URL, SHA256 a případný očekávaný SHA256. Souborová evidence má `url_origin` a stav
downloaded snapshot. I když má pracovní adresář nějakého Git předka, vzdálenému
manifestu se nepřipíše jeho commit. Kontrola evidence ověřuje i soulad skutečně
přečtených bajtů s tímto záznamem. První přečtené bajty zůstávají zachované pro
opakovaná čtení během jednoho buildu.

Dosavadní kontrolované lokální Git publikování zahrne bezpečné URL/checksum
metadata do záznamu revize. Privátní snapshoty ani přihlašovací údaje se do
deployment repozitáře nepřenášejí. ZIP dál obsahuje výstupní YAML podle stávajícího
kontraktu, nikoliv celý privátní pracovní adresář. Načtení URL ani samotný build
neprovádí commit, push nebo nasazení do clusteru.

Přihlašovací údaje se použijí jen při konkrétním stažení. Nejsou v `build.json`,
evidenci, uloženém výběru nebo výsledné odpovědi. Webová pole s tokenem/heslem
se po předání požadavku vyprázdní a hodnoty se neukládají do browser storage.
Čtecí účet ani token se nepřebírá z jiných nastavení builderu.

## Jednorázová URL a konfigurované zdroje

Jednorázové načtení z URL je dostupné ve WebUI. CLI přijímá místní soubor
přes `--release-manifest` nebo načte release z předem určeného úložiště přes
`--release-source-config`. Obě rozhraní používají stejný renderer.

Ověření používá syntetické manifesty a místní HTTPS servery s vlastní CA,
nikoliv konkrétní firemní Artifactory. Stávající omezení lokálního webUI
ohledně autentizace a trusted proxy zůstávají beze změny.

Pro opakované načítání lze [nastavit HTTPS zdroj, připojený Git checkout nebo
vzdálený Git](KUBE_BUILD_APP_CONFIGURED_SOURCE.cs.md). Jednorázový formulář
používá pouze přístupy zadané pro konkrétní požadavek.
