# kube-build-app

`kube-build-app` generuje Kubernetes manifesty z environment repozitáře.

Nástroj má jednoduchý runtime: načte deklarativní metadata prostředí, app modely a assety, a zapíše Kubernetes YAML do cílového adresáře. Ruby implementace je historická reference; Go implementace je produktizované CLI jako single binary, s Cobra commandy a shell completion.

Repozitář teď obsahuje tři související binárky, ale pouze dvě aktivní produktové plochy:

```text
kube-build-app = build/render Kubernetes manifestů
kube-edit-app  = webový editor environment repozitářů
kube-ops-app   = sandbox prototyp, ne aktivní produktový směr
```

`kube-edit-app` je cílový Go přepis Rust aplikace `kube-environments-ui`. Viz `docs/KUBE_EDIT_APP_PLAN.md`.
`kube-ops-app` zůstává pouze sandbox prototypem. Použitelné read-only koncepty se přesouvají do `kube-edit-app`; sync/reconcile zůstává odpovědností ArgoCD. Viz `docs/KUBE_OPS_TO_EDIT_APP_MIGRATION_PLAN.md`.

## kube-edit-app Workflow

Spuštění webového editoru v bezpečném read-only režimu:

```bash
kube-edit-app serve --root ./environments
```

Explicitní povolení strukturovaných zápisů:

```bash
kube-edit-app serve --root ./environments --allow-write
```

Doporučený postup editace:

1. Vybrat environment.
2. Otevřít `Apps` pro app model, `Defaults` pro `_defaults.yml` a sdílené definice nebo `Assets` pro environment JSON a soubory.
3. Ve write režimu provést strukturovanou editaci. Aktuální editory pokrývají local vars, defaults vars/container envs, container profily (jeden záložkový formulář se společným Save/Cancel pro image, startup, envs, CPU/memory, HTTP probes a porty/služby), sdílené sidecar definice, reference, replicas, autoscaling, resources, Java runtime, probes, ports/services/ingress, container envs a special env JSON entries. Položka `_defaults.yml` v Assets je pouze read-only souhrn zdroje s odkazem zpět do Defaults.
4. Opravit případné chyby podle inline validačních hlášek přímo u polí.
5. Otevřít `Build`, spustit validaci a případně vykreslit build preview file tree.
6. Otevřít `Changed files`, rozbalit inline diffy a vybrat soubory k přijetí.
7. Použít `Accept selected`, které commitne jen vybrané dirty soubory. UI zobrazí výsledný commit hash a commitnuté cesty.

Read-only režim stále zobrazuje strukturované preview, build checks, generated file preview a diffy, ale mutační prvky jsou schované nebo vypnuté a mutační API endpointy vrací `403`. Je to preferovaný režim pro review, L2 kontrolu a dashboardy. `--allow-write` používejte jen pro záměrné editace repozitáře.

Pro repozitář, kde každé prostředí potřebuje jiné build vstupy, použijte
serverovou mapu build kontextů:

```bash
kube-edit-app serve \
  --root ./environments \
  --build-config ./kube-edit-build.yml
```

```yaml
environments:
  dev:
    env_url: https://config.example.test/dev/render
    env_url_headers:
      - 'Authorization: Bearer token'
    release_manifest: releases/dev.yml
    resource_policy_root: policies
    image_policy: strict
  test:
    env_file: inputs/test.env
```

Mapa musí obsahovat právě všechny environmenty nalezené pod `--root`.
Neznámé klíče a neplatné kontexty ukončí server ještě před otevřením HTTP
listeneru. Relativní cesty se vyhodnocují vůči adresáři build configu.
`--build-config` nelze kombinovat s per-build CLI flagy; serverové, auth,
EncJson a kubeconfig volby zůstávají globální. Kompletní schema a bezpečnostní
pravidla jsou v
[`docs/KUBE_EDIT_APP_BUILD_CONTEXT.cs.md`](docs/KUBE_EDIT_APP_BUILD_CONTEXT.cs.md).

Za path-based reverse proxy použijte `--base-path /editor`; celé UI, statické
soubory i API pak běží pod `/editor/`.

### Trusted Proxy Autentizace

`kube-edit-app` lze chránit přes důvěryhodnou reverse proxy, která provede
autentizaci v browseru a předá identitu přes `X-Auth-*` hlavičky:

```bash
kube-edit-app serve \
  --root ./environments \
  --trusted-proxy-auth
```

Podporované jsou i environment proměnné:

```bash
KUBE_EDIT_TRUSTED_PROXY_AUTH=true
KUBE_EDIT_AUTH_HEADER_USER=X-Auth-User
KUBE_EDIT_AUTH_HEADER_EMAIL=X-Auth-Email
KUBE_EDIT_AUTH_HEADER_GROUPS=X-Auth-Groups
KUBE_EDIT_AUTH_GROUP_PREFIX=kube-edit-app
```

Podporované skupiny:

- `kube-edit-app:role:admin`
- `kube-edit-app:env:*:reader`
- `kube-edit-app:env:*:writer`
- `kube-edit-app:env:<env>:reader`
- `kube-edit-app:env:<env>:writer`

`reader` může prohlížet povolené environmenty. `writer` může povolené environmenty
měnit, pokud je server zároveň spuštěný s `--allow-write`. Globální Git commit/restore
operace vyžadují `kube-edit-app:role:admin`.

`env.secured.json` lze editovat přes EncJson flow, pokud je `kube-edit-app` spuštěný s nakonfigurovanými EncJson cestami. Existující encrypted hodnoty se zachovají; nově přidané plaintext hodnoty zůstávají pro zašifrování nástrojem EncJson.

## Cíl Metamodelu

`kube-build-app` není Helm. App model záměrně používá pravidlo 80/20:

```text
80 % běžných deployment potřeb = jasná first-class pole metamodelu
20 % speciálních Kubernetes případů = explicitní raw escape hatch
```

Preferujte dedikovaná modelová pole, pokud existují, protože se dají validovat, sumarizovat a editovat přes `kube-edit-app`. `raw:` používejte jen pro Kubernetes pole, která jsou příliš speciální nebo vzácná pro běžný model.

Dlouhodobý plán metamodelu je v `docs/KUBE_BUILD_APP_METAMODEL_PLAN.md`.

## Rychlý Start

Minimální struktura repozitáře:

```text
environments/
  test/
    env.unsecured.json
    apps/
      api.yml
    assets/
```

`environments/test/env.unsecured.json`:

```json
{
  "environment": {
    "NAMESPACE": "demo-test",
    "TSM_REGISTRY_URL": "registry.example.com/demo",
    "TSM_RELEASE_ID": "2026.05.13.1"
  }
}
```

`environments/test/apps/api.yml`:

```yaml
name: api
replicas: 1
containers:
  - name: api
    image: "{{env:TSM_REGISTRY_URL}}/api:{{env:TSM_RELEASE_ID}}"
```

Vygenerování manifestů:

```bash
kube-build-app build -e test -R environments -t deploy/test
```

Výstup:

```text
deploy/test/
  deployments/api-deployment.yml
  services/
  assets/
```

Pokud chybí `-t/--target`, výchozí výstupní adresář je:

```text
<root>/<environment>/target
```

## CLI

Preferované moderní commandy:

```bash
kube-build-app build -e test -R environments -t deploy/test
kube-build-app validate -e test -R environments
kube-build-app summary -e test -R environments
kube-build-app summary --summary-format json -e test -R environments
kube-build-app inventory -e test -R environments
kube-build-app list -e test -R environments
kube-build-app scaffold app api -e test -R environments
kube-build-app import -f deployment.yml -o environments/test/apps/api.yml
kube-build-app completion zsh
```

### Externí Policy Prostředků

Vlastnictví prostředků lze oddělit od repozitáře prostředí pomocí argumentu
`-P/--resource-policy-root`:

```bash
kube-build-app build \
  -e test \
  -R tsm-environments \
  -P tsm-resources \
  -t deploy/test
```

Policy repozitář kopíruje cesty app souborů z environment repozitáře:

```text
tsm-resources/
  test/
    apps/
      tsm-gateway.yml
```

`tsm-resources/test/apps/tsm-gateway.yml` obsahuje pouze prostředky indexované
výsledným názvem containeru:

```yaml
containers:
  tsm-gateway:
    cpu:
      from: "100m"
      to: "500m"
    memory:
      from: "550Mi"
      to: "850Mi"

  tsm-ui-diag:
    cpu:
      from: "10m"
      to: "100m"
    memory:
      from: "20Mi"
      to: "100Mi"
```

Bez `-P` zůstává stávající chování inline bloků `resources`. S `-P` je externí
policy striktní a autoritativní: každý aktivní app model musí mít policy soubor,
každý primární container a sidecar musí mít záznam a neznámé containery způsobí
chybu validace. Externí prostředky plně nahrazují inline hodnoty; neslučují se.
Hodnoty CPU a memory `from`/`to` jsou povinné. Volitelně lze stejným způsobem
přidat `ephemeral-storage`. Generované init containery, tools a fetchery runtime
assetů si ponechávají vlastní konfiguraci prostředků.

Stejnou policy kontrolují příkazy `build`, `validate`, `summary`, `inventory` a
`list`. Inventory označí efektivní hodnoty položkou
`"resources_source": "external-policy"`.

Samostatný repozitář je skutečnou governance hranicí pouze tehdy, když chráněné
CI/CD joby vždy spouštějí `kube-build-app` s povinným argumentem `-P` a přístup
k policy repozitáři je řízen odděleně.

### Generator Commandy

`scaffold` je neinteraktivní generátor souborů prostředí a aplikačních modelů.
Používá stejný princip jako generátory ve frameworku: vytvoří explicitní běžný
YAML metamodelu kube-build-app, který lze následně zkontrolovat a upravit.

Vytvoření základního prostředí:

```bash
kube-build-app scaffold env \
  --root environments \
  --env dev \
  --namespace app-dev \
  --registry-url registry.example.com/project \
  --release-id latest
```

Vytvoří:

```text
environments/_scaffold.yml
environments/dev/env.unsecured.json
environments/dev/apps/_defaults.yml
environments/dev/apps/_scaffold.yml
```

Kořenový `_scaffold.yml` se vytvoří pouze v případě, že dosud neexistuje.
Založení dalšího prostředí jej nepřepíše ani při použití `--force`. Soubor
konkrétního prostředí je na začátku prázdná šablona určená pro odchylky.

Vytvoření minimálního aplikačního modelu:

```bash
kube-build-app scaffold app api \
  --root environments \
  --environment dev \
  --cpu-from 100m \
  --cpu-to 500m \
  --memory-from 128Mi \
  --memory-to 512Mi
```

Generovaný model používá kanonický formát prostředků:

```yaml
name: api
replicas: 1
containers:
  - name: api
    image: "{{REGISTRY_URL}}/api:{{RELEASE_ID}}"
    resources:
      cpu:
        from: 100m
        to: 500m
      memory:
        from: 128Mi
        to: 512Mi
```

Image, název kontejneru a počet replik lze změnit:

```bash
kube-build-app scaffold app worker \
  --root environments \
  --environment dev \
  --image 'custom/worker:1.0.0' \
  --container worker \
  --replicas 2
```

Argument `--dry-run` vypíše generovaný YAML bez zápisu souboru. Generátor
nepřepisuje existující soubory bez argumentu `--force`.

Původní příkazy zůstávají dostupné jako kompatibilní aliasy:

```bash
kube-build-app skeleton env ...
kube-build-app app add api ...
```

Nová automatizace má používat `scaffold env` a `scaffold app`.

#### Profily Pro Konkrétní Repozitář

Způsob spouštění aplikací se mezi repozitáři liší. Image vytvořený pomocí JIB,
samostatný JAR, C++ aplikace a utilita založená na nginx mohou používat odlišné
spouštěcí wrappery. Společné konvence repozitáře patří do:

```text
<kořen-environments>/_scaffold.yml
```

Konkrétní prostředí je může volitelně změnit v:

```text
<kořen-environments>/<prostředí>/apps/_scaffold.yml
```

Oba soubory používá pouze příkaz `kube-build-app scaffold app`. Nejsou součástí
build metamodelu, neslučují se s `_defaults.yml` a jejich změna nemění již
existující aplikační modely. Kořenový soubor leží mimo adresář `apps` a soubor
prostředí začíná znakem `_`, takže se žádný z nich nenačítá jako aplikační model.

Globální příklad:

```yaml
version: 1

defaults:
  replicas: 1
  resources:
    cpu:
      from: 100m
      to: 500m
    memory:
      from: 128Mi
      to: 512Mi

profiles:
  java-jib:
    runtime: java-jib
    image: "{{REGISTRY_URL}}/${APP_NAME}:{{RELEASE_ID}}"
    wrapper:
      source: shared
      path: /app/start-java.sh
      arguments:
        - /app/jib-classpath-file
        - /app/jib-main-class-file
    assets:
      - file: spring_configs/${APP_NAME}.json.tpl
        to: /app/${APP_NAME}.json.tpl
        transform: true

  java-jar:
    runtime: java-jar
    wrapper:
      source: shared
      path: /app/start-java.sh

  cpp:
    runtime: cpp
    wrapper:
      source: shared
      path: /app/start-cpp.sh

  binary:
    runtime: binary
    wrapper:
      source: image
      path: /usr/local/bin/${APP_NAME}
```

Soubor konkrétního prostředí má obsahovat pouze skutečné odchylky:

```yaml
version: 1
profiles:
  java-jib:
    resources:
      memory:
        to: 2Gi
```

Použití profilu:

```bash
kube-build-app scaffold app tsm-calendar \
  --root environments \
  --environment test \
  --scaffold-profile java-jib
```

Pořadí skládání konfigurace je:

1. výchozí hodnoty CLI
2. sekce `defaults` v kořenovém `_scaffold.yml`
3. sekce `defaults` v `_scaffold.yml` konkrétního prostředí
4. vybraný globální profil
5. vybraný profil konkrétního prostředí
6. explicitně zadané argumenty CLI

U profilu stejného názvu přepisují skalární hodnoty a jednotlivé hodnoty
prostředků z konkrétního prostředí globální konfiguraci. Asset se nahrazuje
podle cílové cesty `to`. Sidecary se rekurzivně slučují podle `name`, takže lze
změnit pouze image a zdědit globální startup a resources. Assety a sidecary bez
odpovídajícího zděděného klíče se připojí.

Profily mohou obsahovat také běžné fragmenty metamodelu, které se vloží do
generovaného modelu:

```yaml
profiles:
  java-http:
    app:
      registry:
        - secret_name: docker-registry
      labels:
        app.kubernetes.io/component: ${APP_NAME}
    container_defaults:
      envs:
        - name: LOG_LEVEL
          value: info
      ports:
        - name: http
          port: 8080
          expose_as:
            - hostname: ${APP_NAME}
              port: 80
      probes:
        preset: spring-actuator
        port: 8080
```

Sekce `app` se sloučí na úrovni aplikace. `container_defaults` se sloučí do
generovaného hlavního kontejneru. Obě sekce přímo používají existující build
metamodel a nezavádějí další abstrakci. Základní položky generátoru jsou
vyhrazené: `app` nesmí definovat `name`, `replicas`, `containers` ani `sidecars`
a `container_defaults` nesmí definovat `name`, `image`, `startup`, `assets` ani
`resources`.

Scaffold zná pouze tokeny `${APP_NAME}` a `${CONTAINER_NAME}`. Nahradí je při
generování souboru. Existující placeholdery metamodelu `{{REGISTRY_URL}}`,
`{{RELEASE_ID}}`, `{{env:VAR}}` a `{{var:VAR}}` zůstávají beze změny pro
standardní build nebo fázi nasazení.

Hodnota `runtime` je pouze pokyn pro scaffold a do výsledného aplikačního modelu
se nezapisuje. Podporované hodnoty jsou `java-jib`, `java-jar`, `cpp`, `binary`
a `custom`. Velikost Java heap se z Kubernetes memory limitu záměrně
neodvozuje. Pokud je potřeba, nastavte ve výsledném modelu explicitně
`runtime.java`.
Položky specifické pro scaffolding se načítají striktně, takže neznámé položky
a běžné překlepy způsobí chybu ještě před zápisem aplikačního souboru. Obsah
sekcí `app` a `container_defaults` je běžný fragment build metamodelu a má se
kontrolovat příkazem `kube-build-app validate`.

#### Zdroje Wrapperu

Profil nebo CLI může zvolit jeden ze tří zdrojů wrapperu:

- `shared`: cílovou cestu wrapperu již musí poskytovat
  `<environment>/shared.assets.yml`; aplikační asset se nevygeneruje
- `asset`: `wrapper.file` je relativní k `<environment>/assets`; generátor přidá
  mapování aplikačního assetu do `wrapper.path`
- `image`: spustitelný soubor nebo skript již existuje v image kontejneru;
  žádný asset se nevygeneruje

Příklad profilu s wrapperem vlastněným aplikací:

```yaml
profiles:
  nginx:
    runtime: custom
    wrapper:
      source: asset
      file: utils/start-nginx.sh
      path: /app/start-nginx.sh
```

Ekvivalentní volání CLI:

```bash
kube-build-app scaffold app edge-proxy \
  -e test -R environments \
  --runtime custom \
  --wrapper-source asset \
  --wrapper-file utils/start-nginx.sh \
  --wrapper-path /app/start-nginx.sh
```

Wrappery pro `java-jib`, `java-jar`, `cpp` a `custom` se ve výchozím nastavení
spouštějí pomocí `/bin/sh`. Wrapper typu `binary` nebo `image` se spouští přímo.
Jiný spouštěcí program lze zadat opakovatelným argumentem `--wrapper-command`;
argumenty wrapperu zadává `--wrapper-arg`. Při použití spouštěcího programu se
cesta wrapperu stane prvním argumentem.

#### Assety A Sidecary

Assety vlastněné aplikací lze přidat opakovatelným argumentem:

```bash
kube-build-app scaffold app api \
  -e test -R environments \
  --asset config/api.yml=/app/config.yml \
  --asset ssl/truststore.p12=/app/ssl/truststore.p12
```

Zdroj je vždy relativní k `<environment>/assets` a cíl musí být absolutní cesta
v kontejneru. Generátor odmítne absolutní zdroj, průchod přes `..`, chybějící
zdrojový soubor, symbolický odkaz směřující mimo adresář assetů a duplicitní
cílovou cestu.

Jednoduché sidecary lze přidat z CLI:

```bash
kube-build-app scaffold app api \
  -e test -R environments \
  --sidecar metrics=registry.example.com/metrics:1 \
  --sidecar audit=registry.example.com/audit:2
```

Sidecary z CLI dostanou výchozí prostředky nastavitelné pomocí argumentů
`--sidecar-cpu-from`, `--sidecar-cpu-to`, `--sidecar-memory-from` a
`--sidecar-memory-to`. Standardní sidecar repozitáře se spouštěním, proměnnými,
mounty nebo dalšími vlastnostmi je vhodné zapsat do profilu jako úplný fragment
existujícího sidecar metamodelu:

```yaml
profiles:
  java-jib:
    sidecars:
      - name: cgroup-runtime-exporter
        image: "{{REGISTRY_URL}}/cgroup-runtime-exporter:{{RELEASE_ID}}"
        startup:
          command:
            - /usr/local/bin/cgroup-runtime-exporter
        envs:
          - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
            value: java
        resources:
          cpu:
            from: 5m
            to: 25m
          memory:
            from: 8Mi
            to: 32Mi
```

Vygenerovaný sidecar zůstává běžnou položkou metamodelu `sidecars`. Scaffold
profily nezavádějí druhou reprezentaci pro běh aplikace.

### Import Deploymentu

Z existujícího `apps/v1` Deploymentu lze vytvořit výchozí app model:

```bash
kube-build-app import \
  --file deployment.yml \
  --output environments/test/apps/api.yml
```

Vstup lze číst také ze stdin:

```bash
kubectl -n demo get deployment api -o yaml \
  | kube-build-app import --file - --output environments/test/apps/api.yml
```

Při přihlášení do aktuálního Kubernetes contextu může `kube-build-app` spustit
`kubectl` přímo. V tomto režimu načte Deployment a Services ve zdrojovém
namespace:

```bash
kube-build-app import \
  --namespace demo \
  --deployment api \
  --output environments/test/apps/api.yml
```

Import report se vytvoří pouze při explicitním zadání:

```bash
kube-build-app import \
  --file deployment.yml \
  --output environments/test/apps/api.yml \
  --report api.import-report.json
```

Chování importu:

- všechny Kubernetes containery importuje do `containers`; jejich roli sidecaru
  nikdy neodhaduje
- init containery, resources, environment reference, probes, scheduling a běžná
  nastavení podu převádí do strukturovaných polí metamodelu
- Kubernetes requests a limits zapisuje v kanonickém formátu metamodelu `from`
  a `to`
- clusterový import porovná selectory Services s labely pod template a
  jednoznačné porty převede na pojmenované `ports` a
  `expose_as[].service_name`
- původní selector Deploymentu zachová přes `selector_labels`, takže
  znovu vytvořený Deployment i generované Services používají původní selector
- defaulty doplněné Kubernetes API vynechá, pokud má metamodel stejný výsledný
  default; týká se to výchozího ServiceAccountu, probe thresholds a běžných
  defaultů Deploymentu, podu a containeru
- hodnoty zachová, pokud by jejich vynechání změnilo chování; například
  `image_pull_policy: IfNotPresent` zůstává, protože metamodel má default
  `Always`, a `replicas: 1` je explicitní, protože `0` znamená zastavenou aplikaci
- nepodporovaná, ale znovu použitelná pole zachová přes `deployment_raw`,
  `pod_raw` a container `raw`
- offline import přes `--file` nebo stdin čte pouze Deployment, a proto z něj
  nemůže odvodit Services
- Ingresses, Routes, HPA ani ConfigMaps zatím neimportuje
- reference na Secret importuje, ale hodnoty Secretů nikdy nepožaduje ani nečte

Vygenerovaný model je kontrolovatelný výchozí bod migrace. Není důkazem, že lze
původní objekt zrekonstruovat shodně po jednotlivých bajtech.

Legacy root flagy zůstávají kvůli kompatibilitě:

```bash
kube-build-app -e test -R environments -t deploy/test
kube-build-app -s -e test -R environments
kube-build-app -i -e test -R environments
kube-build-app -l -e test -R environments
```

Nemíchejte action subcommandy s legacy action flagy. Tohle je záměrně nevalidní:

```bash
kube-build-app build -e test -s
```

Použijte:

```bash
kube-build-app summary -e test
```

Verbose build logování:

```bash
kube-build-app build -e test -R environments -t deploy/test --verbose
kube-build-app build -e test -R environments -t deploy/test --verbose --log-format json
kube-build-app build -e test -R environments -t deploy/test --verbose --color always
```

Build logy se zapisují na `stderr`. Normální výstup příkazu zůstává na `stdout`.

Užitečné flagy:

```text
-e, --environment        název prostředí
-R, --root               root adresář environments, výchozí: environments
-t, --target             výstupní adresář
-p, --profile            název replica profilu
    --profiles-file      cesta k replica profiles souboru
-r, --release-manifest   cesta k release manifest YAML; opakovatelné, pozdější má přednost
    --release-id         ID výsledného deploymentu; povinné pro více manifestů
    --image              přepíše image ve formátu app/container=image; opakovatelné
    --image-policy       politika image override: fallback nebo strict
    --image-reference    release image reference: auto, digest nebo tag
    --force-image-tag    vynutí jeden tag pro všechny image z release manifestu
    --force-image-prefix nahradí prefix release images a zachová basename
-w, --down               nastaví vybraným appkám replicas na 0
-E, --env-file           explicitní .env soubor
    --legacy-apply-env   nahradí legacy {{VAR}} v deployment a external service manifestech
    --env-url            HTTP(S) URL vracející .env obsah
    --env-url-header     HTTP hlavička pro --env-url ve formátu 'Name: value'; opakovatelné
    --env-url-insecure   přeskočí ověření TLS certifikátu pro --env-url
    --vars-source        env, json, dot-env; opakovatelné nebo comma-separated
-d, --decrypt-secured    zapne proměnné z env.secured.json
    --helm-escape-assets escapuje zbývající {{VAR}} placeholdery v textových assetech
    --verbose            vypíše build render eventy na stderr
    --log-format         formát verbose build logu: text nebo json
    --color              barvy ve verbose text logu: auto, always nebo never
```

## Vyhodnocení Image


Podporované jsou `oci-toolbox/v1` (`image`, `source.image`, `registry_base`),
`oci-toolbox/v2` (`repository`, `source.repository`, `repository_prefix`) i
historické v1 soubory bez hlavičky. Názvy polí musí odpovídat verzi.
Duplicitní selectory a ID jsou odmítnuty uvnitř jednoho souboru; mezi soubory
se stejné selectory mohou přepisovat a logická ID položek opakovat.

Opakovaný `--release-manifest` aplikuje soubory v pořadí argumentů. Pozdější
soubor přepíše jen containery, pro které obsahuje odpovídající záznam, včetně
wildcardu a app-level fallbacku. Uvnitř každého souboru má přesný selector
přednost před wildcardem a app-level fallbackem. Explicitní `--image` se
aplikuje nakonec a může dodat image pro selector chybějící ve všech manifestech.
Cílí na existující containery/sidecary; nové containery nevytváří.

```bash
kube-build-app build -e prod -R environments -t deploy/prod \
  --release-manifest releases/core.yml \
  --release-manifest releases/addons.yml \
  --release-id TSM-Core_RE2026.01.02 \
  --image 'monitoring/nginx=docker.io/library/nginx:1.27'
```

Více souborů vyžaduje explicitní `--release-id` pro deployment labely,
release-context ConfigMap a build proměnnou `RELEASE_ID`. Jeden soubor dál
poskytne své `release_id` automaticky. Explicitní `--release-id` má přednost
před `RELEASE_ID` ze zdrojů proměnných; bez tohoto argumentu zůstává původní
kontrola konfliktu. Veřejné images mohou používat také `@sha256:...`.
Builder nepřistupuje do registry.

`kube-edit-app` používá stejné vyhodnocení. Jeho build config navíc podporuje
uspořádané `release_manifests: [core.yml, addons.yml]` a `release_id`.
Původní `release_manifest` zůstává podporovaný. Relativní cesty se vyhodnotí
vůči build configu; při uvedení obou klíčů se nejprve aplikuje samostatný
soubor a potom seznam.


`kube-build-app` vyhodnocuje container image podle dvojice `<app>/<container>`.

Pořadí precedence:

1. `--image app/container=image`
2. `--release-manifest release.yml`
3. `image:` z `<env>/apps/<app>.yml`

Přímý CLI override se hodí pro jednorázové CI/CD joby:

```bash
kube-build-app build -e test \
  --image 'tsm-dms/tsm-dms=registry.example.com/tsm-dms:2.0.0' \
  --image 'tsm-ui/tsm-ui=registry.example.com/tsm-ui@sha256:abcdef'
```

Mezi selektorem a image používáme `=`, ne `:`, protože container image reference sama používá `:` pro tagy a `@sha256:...` pro digesty.

Release manifest je určený pro řízené release pipeline. `kube-build-app` umí přímo použít neměnný manifest generovaný přes `oci-toolbox bundle publish` nebo `oci-toolbox release reconstruct`:

```yaml
apiVersion: oci-toolbox/v2
kind: ImageRelease
release_id: RE_2026.07.28.01
created_at: 2026-07-28T18:00:00Z
bundle:
  name: stable
  revision: abc123
repository_prefix: registry.example.com/project
platform: linux/amd64
images:
  - id: api
    app_name: api
    container_name: api
    source:
      repository: registry-source.example.com/team/api
      tag: build-1
      digest: sha256:source
    repository: registry.example.com/project/api
    tag: RE_2026.07.28.01
    digest: sha256:target
    extra_tags: [stable]
    platform: linux/amd64
  - app_name: "*"
    container_name: cgroup-runtime-exporter
    repository: registry.example.com/project/cgroup-runtime-exporter
    tag: RE_2026.07.28.01
extra_tags: [stable]
```

Parser je striktní a rozumí auditním metadatům zapisovaným nástrojem `oci-toolbox`: `created_at`, `bundle`, `platform`, image `id`, `source` a `extra_tags`. Pro rendering používá pouze `app_name`, `container_name`, cílové `repository`, `digest` a `tag`. Duplicitní selectory a ID jsou odmítnuty stejně jako per-image platforma, která je v konfliktu s top-level platformou.

Pro container nebo sidecar sdílený více aplikacemi použijte `app_name: "*"`.
Přesný záznam `<app_name>/<container_name>` má před wildcardem přednost, takže
konkrétní aplikace může použít jinou image. App-level záznam bez
`container_name` zůstává fallbackem pro primární containery aplikace a
sidecary nepřepisuje.

Pokud je vyplněný `digest`, výsledný deployment použije neměnnou digest referenci:

```text
registry.example.com/project/tsm-dms@sha256:abcdef
```

Pokud je vyplněný pouze `tag`, výsledný deployment použije:

```text
registry.example.com/project/tsm-dms:2026.06.25.01
```

Pokud release manifest obsahuje `release_id`, `kube-build-app` ho také
zpřístupní jako build-time proměnnou `RELEASE_ID`. Release metadata tak není
nutné duplikovat v `.env` souboru:

```yaml
labels:
  app.kubernetes.io/version: "{{env:RELEASE_ID}}"
```

Pokud externí zdroj proměnných rovněž definuje `RELEASE_ID`, musí se jeho
hodnota shodovat s release manifestem. Při rozdílu build skončí chybou, aby
nevznikla image a metadata s rozdílnými verzemi. Bez `--release-manifest`
pochází `RELEASE_ID` nadále pouze z nakonfigurovaných externích zdrojů
proměnných.

Image policy:

```bash
kube-build-app build -e test --release-manifest release.yml --image-policy fallback
kube-build-app build -e test --release-manifest release.yml --image-policy strict
kube-build-app build -e test --release-manifest release.yml --image-reference tag
kube-build-app build -e test --release-manifest release.yml \
  --force-image-prefix artifactory.example.com/docker-release \
  --force-image-tag emergency-1
```

`fallback` ponechá image z app YAML, pokud override neexistuje. `strict` vyžaduje, aby každý primární app `containers[]` image měl záznam v `--image` nebo `--release-manifest`; to je doporučené pro release pipeline. Sidecar image lze z release manifestu také přepsat, ale `strict` je nevyžaduje, protože často jde o deterministické platformní/helper image z app modelu.

`--image-reference auto` je výchozí a preferuje neměnný digest, potom tag a nakonec samotný název image. Režimy `digest` a `tag` explicitně vyžadují příslušný typ reference u každé nalezené image a při jeho absenci skončí chybou.

Force argumenty slouží jako explicitní záchranný režim. `--force-image-prefix` nahradí celý repository prefix a zachová pouze poslední basename image. `--force-image-tag` potom nahradí všechny digesty nebo tagy z manifestu jedním společným tagem. Ovlivňují pouze images vybrané z `--release-manifest`; per-container override `--image` má stále nejvyšší prioritu. Prefix používá OCI syntaxi bez `https://` nebo `docker://`. `--force-image-tag` nelze kombinovat s `--image-reference digest`.

Repository se stejným basename se při vynuceném prefixu namapují na stejný cíl. Například `team-a/api` i `team-b/api` skončí jako `<vynucený-prefix>/api`; toto akceptované omezení je potřeba ověřit na testovacím prostředí před nasazením do produkce.

## Kontrakt Placeholderů

Existují tři formy placeholderů. Mají odlišný scope a odlišný čas vyhodnocení.

| Placeholder | Kdo řeší | Kdy | Význam |
| --- | --- | --- | --- |
| `{{VAR}}` | `apply-env`, CI/CD, deploy fáze, Helm-safe post-processing | později | Runtime/deploy placeholder. `kube-build-app` ho v app YAML nechá beze změny. |
| `{{env:VAR}}` | `kube-build-app` | build time | Build-time environment proměnná načtená z env JSON, `.env` nebo process env podle nastavených zdrojů. |
| `{{var:VAR}}` | `kube-build-app` | build time | App-local proměnná z bloku `vars:` v aktuálním `<app>.yml`, po mergi `_defaults.yml`. |

Pravidlo:

```text
prefixovaný placeholder = vyřešit při kube-build-app
holý placeholder        = nechat na pozdější deploy/render fázi
```

Příklad:

```yaml
vars:
  - name: APP_NAME
    value: api

name: "{{var:APP_NAME}}"
containers:
  - name: "{{var:APP_NAME}}"
    image: "{{env:TSM_REGISTRY_URL}}/{{var:APP_NAME}}:{{env:TSM_RELEASE_ID}}"
    envs:
      - name: RUNTIME_VALUE
        value: "{{RUNTIME_VALUE}}"
```

Ve výsledném deploymentu zůstane jen holý runtime placeholder:

```yaml
containers:
  - name: api
    image: registry.example.com/demo/api:2026.05.13.1
    env:
      - name: RUNTIME_VALUE
        value: "{{RUNTIME_VALUE}}"
```

### Proměnné Prostředí Kontejneru

Pro proměnné prostředí kontejneru používejte `containers[].envs`. Historický
zápis `containers[].env_vars` zůstává kvůli existujícím environment repozitářům
funkční trvale, ale nástroj při jeho použití vypíše deprekační varování.

Pokud jsou v jednom kontejneru oba bloky, hodnoty se sloučí podle názvu
proměnné: nejprve se použije `env_vars`, při shodném názvu jej přepíše `envs`.

Pro zákaz nového historického zápisu v CI použijte:

```bash
kube-build-app validate -e test -R environments --fail-on-deprecated
```

## Zdroje Proměnných

Výchozí chování je zpětně kompatibilní:

```text
env.unsecured.json + env.secured.json při -d, potom process environment
```

Explicitní `.env` soubor:

```bash
kube-build-app build -e test -E /path/to/release.env
```

Tohle je doporučený produkční workflow pro secured proměnné. Čím se hodnoty dešifrují je mimo `kube-build-app`; nástroj dostane už vyřešené key/value páry.

V tomto režimu je explicitní `.env` jediný zdroj proměnných. Nelze ho kombinovat s `-d` ani s `--vars-source`.

Starší environment repozitáře mohou po načtení explicitního `.env` souboru zapnout
kompatibilní post-processing:

```bash
kube-build-app build -e test \
  -E /path/to/release.env \
  --legacy-apply-env
```

`--legacy-apply-env` nahrazuje holé `{{VAR}}` pouze ve vygenerovaných souborech pod
`deployments/` a `services/external/`, tedy ve stejném rozsahu jako historický
post-build skript s nástrojem `apply-env`. Neznámá proměnná nebo neplatný výsledný YAML
ukončí build chybou. Bez tohoto explicitního přepínače zůstává standardní placeholder
kontrakt beze změny a holé placeholdery se ponechají pro pozdější fázi.

Vzdálený `.env` zdroj:

```bash
kube-build-app build -e test \
  --env-url "https://server/public/v1/tenants/acme/environments/test/export-profiles/default/render" \
  --env-url-header "Authorization: Bearer $TOKEN"
```

V tomto režimu je stažený `.env` jediný zdroj proměnných. Nelze ho kombinovat s `-E`, `-d` ani s `--vars-source`.
Pro interní self-signed HTTPS endpointy přidejte `--env-url-insecure`, aby se přeskočilo ověření TLS certifikátu.

Přepsání cílového namespace:

```bash
kube-build-app build -e test --namespace customer-test
```

`--namespace` má nejvyšší prioritu a přepisuje `NAMESPACE` ze všech zdrojů
proměnných, včetně `--env-file` a `--env-url`. Bez tohoto argumentu zůstává
stávající chování proměnné `NAMESPACE` beze změny. Prostředí vybrané pomocí
`-e/--environment` a výsledný Kubernetes namespace jsou nezávislé hodnoty.

Zpětně kompatibilní decrypt secured JSON:

```bash
kube-build-app build -e test -d
```

Při použití `-d/--decrypt-secured` se `env.secured.json` dešifruje spuštěním externí EncJson binárky:

```text
encjson decrypt -k <keydir> -f env.secured.json
encjson-rs decrypt -k <keydir> -f env.secured.json
```

Výběr binárky:

```text
EncJson[@api=1.0  -> ENCJSON_LEGACY_PATH, ENCJSON_LEGACY_BIN, ENCJSON_BIN, fallback encjson
EncJson[@api=2.0  -> ENCJSON_PATH, ENCJSON_RS_BIN, fallback encjson-rs
neznámý marker    -> ENCJSON_BIN nebo legacy fallback
```

Adresář s klíči:

```text
pokud je ENCJSON_KEYDIR nastavené: decrypt -k "$ENCJSON_KEYDIR" -f env.secured.json
jinak:                              decrypt -f env.secured.json
```

Když `ENCJSON_KEYDIR` není nastavené, výběr keydir se nechává na samotné EncJson utilitě.

Explicitní výběr zdrojů:

```bash
kube-build-app build -e test --vars-source json
kube-build-app build -e test --vars-source env
kube-build-app build -e test --vars-source json --vars-source env
kube-build-app build -e test --vars-source json,env
```

Podporované zdroje:

```text
json     env.unsecured.json a env.secured.json při -d
env      process environment
dot-env  <environment_dir>/.env, pokud není použité -E/--env-file
```

## App Model Skládáním

App model je jeden YAML soubor v:

```text
<environment>/apps/<app>.yml
```

Soubory začínající `_` jsou speciální soubory, ne appky. Například:

```text
<environment>/apps/_defaults.yml
```

### 1. Minimální App

```yaml
name: api
containers:
  - name: api
    image: nginx:stable
```

Výsledný deployment obsahuje jeden container:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
spec:
  template:
    spec:
      containers:
        - name: api
          image: nginx:stable
```

### 2. Repliky

Přidejte:

```yaml
replicas: 3
```

Výsledný deployment obsahuje:

```yaml
spec:
  replicas: 3
```

### 3. Resources

Přidejte container resources s Kubernetes slovníkem `requests` / `limits`:

```yaml
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        requests: "100m"
        limits: "500m"
      memory:
        requests: "128Mi"
        limits: "512Mi"
```

Legacy `from` / `to` zůstává podporované:

```yaml
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        from: "100m"
        to: "500m"
      memory:
        from: "128Mi"
        to: "512Mi"
```

Výsledný deployment obsahuje:

```yaml
resources:
  requests:
    cpu: 100m
    memory: 128Mi
  limits:
    cpu: 500m
    memory: 512Mi
```

Summary command tyto hodnoty používá a celkové součty násobí počtem replik:

```bash
kube-build-app summary -e test -R environments
```

#### Autoscaling / HPA

App-level `autoscaling` použijte ve chvíli, kdy má runtime počet replik řídit Kubernetes HPA:

```yaml
replicas: 2
autoscaling:
  enabled: true
  min_replicas: 2
  max_replicas: 6
  cpu:
    average_utilization: 75
  memory:
    average_utilization: 80
containers:
  - name: api
    image: nginx:stable
    resources:
      cpu:
        requests: "100m"
        limits: "500m"
      memory:
        requests: "128Mi"
        limits: "512Mi"
```

Výsledné manifesty obsahují navíc `deployments/<app>-hpa.yml`:

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
spec:
  minReplicas: 2
  maxReplicas: 6
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api
```

Pokud `autoscaling.enabled=true`, runtime počet replik řídí HPA. Hodnota `replicas` zůstává ve výsledném Deployment/StatefulSet jako počáteční požadovaný stav, ale po spuštění objektu ji může HPA měnit.

CPU a memory utilization metriky vyžadují container `resources.requests`. Pro pokročilá HPA pole, například `spec.behavior`, použijte `autoscaling.raw`:

```yaml
autoscaling:
  enabled: true
  min_replicas: 2
  max_replicas: 6
  cpu:
    average_utilization: 75
  raw:
    spec:
      behavior:
        scaleDown:
          stabilizationWindowSeconds: 300
```

### 4. Container Environment Variables

Přidejte:

```yaml
containers:
  - name: api
    image: nginx:stable
    envs:
      - name: JAVA_OPTS
        value: "-Xms256m -Xmx512m"
      - name: POD_NAME
        field_path: metadata.name
```

Výsledný deployment obsahuje:

```yaml
env:
  - name: JAVA_OPTS
    value: "-Xms256m -Xmx512m"
  - name: POD_NAME
    valueFrom:
      fieldRef:
        fieldPath: metadata.name
```

#### Java Runtime Options

Pro Java containery preferujte `runtime.java` pro známé JVM memory parametry místo ručního skládání `JAVA_OPTS`:

```yaml
containers:
  - name: api
    image: nginx:stable
    runtime:
      java:
        xms: "512m"
        xmx: "2048m"
        opts:
          - "-XX:+UseG1GC"
        export:
          env_name: JAVA_OPTS
    resources:
      cpu:
        requests: "100m"
        limits: "1000m"
      memory:
        requests: "1024Mi"
        limits: "2560Mi"
```

Výsledný deployment obsahuje:

```yaml
env:
  - name: JAVA_OPTS
    value: "-Xms512m -Xmx2048m -XX:+UseG1GC"
```

`export.env_name` má default `JAVA_OPTS`. Pokud je stejná proměnná zároveň ručně uvedená v `vars`, validace skončí chybou. Díky tomu je JVM heap sizing viditelný v `kube-build-app summary` a nevznikají skryté konflikty se startup skripty.

### 5. Porty a Services

Přidejte container port:

```yaml
containers:
  - name: api
    image: nginx:stable
    ports:
      - name: http
        port: 8080
```

Deployment dostane `containerPort: 8080`.

Service vznikne přes `expose_as`:

```yaml
ports:
  - name: http
    port: 8080
    expose_as:
      - service_name: api
        port: 80
```

Výsledný service:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: api
spec:
  ports:
    - name: http-80
      port: 80
      targetPort: 8080
```

`service_name` je název Kubernetes Service a zároveň krátké DNS jméno v namespace. Legacy `expose_as[].hostname` je dál podporované jako alias kvůli zpětné kompatibilitě.

Externí vystavení se přidává pod `expose_as[].external` a podle modelu generuje Ingress nebo OpenShift Route.

### 6. Probes

Preferovaný moderní zápis:

```yaml
containers:
  - name: api
    image: api:latest
    probes:
      preset: spring-actuator
      port: 8080
```

Vygeneruje:

```text
livenessProbe  -> /actuator/health/liveness
readinessProbe -> /actuator/health/readiness
startupProbe   -> /actuator/health
```

Obecný HTTP zápis:

```yaml
probes:
  preset: http
  port: 8080
  path: /healthz
```

Zápis s override:

```yaml
probes:
  http:
    path: /actuator/health
    port: 8080
  live:
    period: 10
    timeout: 2
    failure: 5
  ready:
    period: 2
    timeout: 2
    success: 2
    failure: 2
  start:
    period: 10
    timeout: 2
    failure: 30
```

Exec probes:

```yaml
probes:
  live:
    command: ["/bin/sh", "/app/liveness.sh"]
  ready:
    command: ["/bin/sh", "/app/liveness.sh"]
  start:
    command: ["/bin/sh", "/app/liveness.sh"]
    failure: 30
```

Legacy bloky `health` a `probe` zůstávají podporované kvůli zpětné kompatibilitě:

```yaml
containers:
  - name: api
    image: nginx:stable
    health:
      http:
        path:
          live: /health/live
          ready: /health/ready
        port: 8080
```

`health` generuje jen liveness/readiness. `probe` umí liveness/readiness/startup, ale je ukecanější. Nové soubory by měly preferovat `probes`.

### 7. Assets

Soubor vložte do:

```text
<environment>/assets/nginx.conf
```

A referencujte ho z appky:

```yaml
containers:
  - name: api
    image: nginx:stable
    assets:
      - file: assets/nginx.conf
        to: /etc/nginx/conf.d/default.conf
```

Výstup obsahuje:

```text
assets/api-asset-<crc>.yml
```

Deployment dostane volume mount:

```yaml
volumeMounts:
  - mountPath: /etc/nginx/conf.d/default.conf
    name: api-asset-<crc>
    readOnly: true
    subPath: nginx.conf
volumes:
  - name: api-asset-<crc>
    configMap:
      name: api-asset-<crc>
```

Digest v názvu assetu vychází z reálného asset vstupu a cílové mount cesty. ConfigMap jméno je stabilní a mění se jen při změně mountovaného obsahu.

Textové assety mohou použít build-time transform:

```yaml
assets:
  - file: assets/application.yml.tpl
    to: /app/config/application.yml
    transform: true
```

Legacy `assets` podporuje i volume-only formy jako `temp`, `pvc`, `nfs-server` a `host-path`. Kvůli zpětné kompatibilitě zůstávají podporované, ale nové soubory by měly pro volume deklarace preferovat `mounts`.

### 8. Mounts

`mounts` používejte pro non-ConfigMap volumes a pro čistší moderní zápis file mountů:

```yaml
containers:
  - name: api
    image: api:latest
    mounts:
      - type: config
        file: assets/app.conf
        mount_path: /app/app.conf

      - type: empty_dir
        name: cache
        mount_path: /app/cache

      - type: pvc
        name: data
        claim_name: data-claim
        mount_path: /data

      - type: nfs
        name: data-nfs
        server: nfs.local
        path: /export/data
        mount_path: /nfs

      - type: host_path
        name: host-data
        path: /var/lib/host-data
        mount_path: /host
```

`host_path` je high-risk escape hatch, protože containeru vystavuje filesystem path přímo z nodu.

Pro vzácné Kubernetes volume typy použijte raw mount passthrough:

```yaml
mounts:
  - type: raw
    volume:
      name: special
      projected:
        sources: []
    mount:
      name: special
      mountPath: /app/special
      readOnly: true
```

### 9. Shared Assets

`<environment>/shared.assets.yml` slouží pro assety mountované do více appek.
Pokud má na asset odkazovat jiný blok metamodelu, přiřaďte mu `name`:

```yaml
assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
```

Jméno shared assetu je nepovinné, ale každé uvedené jméno musí být unikátní
Kubernetes DNS label. Pole končící na `_ref_name` vyhledá jinou deklaraci podle
jejího `name`; pole končící na `_ref_names` obsahuje seznam takových referencí.

Referenční kontrakt záměrně odstranil nejednoznačné klíče:

| Odstraněný klíč | Náhrada |
|---|---|
| `envs[].workload_identity_token` | `workload_identity_token_ref_name` |
| `runtime_assets` | `runtime_asset_definitions` + `runtime_asset_ref_names` |
| `runtime_assets[].source.token` | `workload_identity_token_ref_name` |
| `runtime_assets[].apps` | app-level `runtime_asset_ref_names` |
| `runtime_assets[].containers` | `containers[].runtime_asset_ref_names` |
| `_defaults.yml sidecars` | `sidecar_definitions` + app-level `sidecar_ref_names` |
| `container_envs[].name` | `container_ref_name` |
| `replica-profiles.yml defaults.profile` | `replica_profile_ref_name` |

Použití odstraněného klíče ukončí validaci s migrační zprávou.

Appka je může vypnout:

```yaml
disable_shared_assets: true
```

### 10. Tools

Statické utility binárky lze zkopírovat initContainery a přimountovat na přesnou cestu do každého aplikačního kontejneru:

```yaml
tools:
  - name: util-apply-env
    image: registry.example.com/tools/apply-env:latest
    expose_bin: /usr/bin/apply-env
    mount_path: /usr/local/bin/apply-env
    image_pull_policy: Always
    resources:
      cpu: { requests: "10m", limits: "100m" }
      memory: { requests: "16Mi", limits: "128Mi" }
```

Chování:

- každý tool se generuje jako initContainer
- initContainer zkopíruje `expose_bin` do sdíleného `emptyDir`
- app containery přimountují zkopírovaný soubor read-only na `mount_path` přes `subPath`
- `mount_path` je povinný a musí být absolutní cesta k souboru
- `image_pull_policy` má výchozí hodnotu `Always`; povolené hodnoty jsou `Always`, `IfNotPresent` a `Never`
- `as` je odstraněné a odmítne se; musí být nahrazeno `mount_path`
- každý tool dostane výchozí resources (`10m`/`100m` CPU a `16Mi`/`128Mi` memory), proto vyhoví namespace s `ResourceQuota`; `resources` může přepsat jednotlivé hodnoty

`image_pull_policy` používá stejné hodnoty a výchozí chování u `containers`,
`sidecars`, `init_containers` i `tools`.

### 11. Scheduling a Pod Metadata

Časté app-level fields:

```yaml
labels:
  team: tsm
annotations:
  app.example.com/owner: l2
pod_annotations:
  prometheus.io/scrape: "true"
arch: amd64
node_selector:
  node-role.kubernetes.io/worker: ""
tolerations:
  - key: dedicated
    operator: Equal
    value: tsm
    effect: NoSchedule
```

Renderují se do deployment metadata a pod template.

Preferovaný moderní zápis pro scheduling:

```yaml
scheduling:
  arch: amd64
  node_selector:
    node-role.kubernetes.io/worker: ""
  tolerations:
    - key: dedicated
      operator: Equal
      value: tsm
      effect: NoSchedule
  spread:
    by: hostname
    max_skew: 1
    when_unsatisfiable: ScheduleAnyway
  anti_affinity:
    self: preferred
    topology: kubernetes.io/hostname
```

Pro vzácné Kubernetes scheduling případy použijte raw affinity pod `scheduling.affinity`:

```yaml
scheduling:
  affinity:
    nodeAffinity:
      preferredDuringSchedulingIgnoredDuringExecution:
        - weight: 10
          preference:
            matchExpressions:
              - key: disk
                operator: In
                values: [ssd]
```

Legacy `arch`, `node_selector` a `tolerations` zůstávají podporované. Nové soubory by měly preferovat `scheduling`.

### 12. Security Context

Pro běžná pod a container security nastavení použijte `security_context`.

App-level `security_context` se renderuje do pod spec `securityContext`:

```yaml
security_context:
  runAsNonRoot: true
  fsGroup: 2000
```

Container-level `security_context` se renderuje do container `securityContext`:

```yaml
containers:
  - name: api
    image: nginx:stable
    security_context:
      allowPrivilegeEscalation: false
      runAsUser: 1000
      capabilities:
        drop: ["ALL"]
```

Preferujte toto dedikované pole před `raw.securityContext`. Pokud se použije obojí, `raw` zůstává kompatibilní výjimka a může vygenerované pole přepsat.

### 13. Lifecycle a Termination Grace

App-level `termination_grace_period` se renderuje do pod spec `terminationGracePeriodSeconds`:

```yaml
termination_grace_period: 45
```

Container-level `lifecycle.pre_stop` použijte pro graceful shutdown hooky:

```yaml
containers:
  - name: api
    image: nginx:stable
    lifecycle:
      pre_stop:
        command: ["/bin/sh", "-c", "sleep 10"]
```

Výsledkem je Kubernetes `lifecycle.preStop.exec.command`.

Pro vzácné lifecycle handler formy použijte explicitní raw passthrough:

```yaml
containers:
  - name: api
    lifecycle:
      pre_stop:
        raw:
          httpGet:
            path: /shutdown
            port: 8080
```

### 14. Service Account a Env From

App-level `service_account` se renderuje do pod spec `serviceAccountName`:

```yaml
service_account: tsm-api
```

Container-level `env_from` použijte pro běžné importy environment proměnných z ConfigMap/Secret:

```yaml
containers:
  - name: api
    image: api:latest
    env_from:
      - config_map: api-config
      - secret: api-secret
        prefix: SECRET_
```

Pro méně běžné Kubernetes volby použijte nativní tvar reference:

```yaml
env_from:
  - configMapRef:
      name: api-config
      optional: true
  - secretRef:
      name: api-secret
      optional: true
```

Výsledkem je Kubernetes `envFrom`.

### 15. Workload Identity a Downward API

`workload_identity` použijte ve chvíli, kdy aplikace potřebuje projektovaný Kubernetes/OpenShift ServiceAccount token pro service-to-service autentizaci:

```yaml
workload_identity:
  service_account:
    create: true
    name: order-api      # default: název appky
    automount: false     # default: false, pokud jsou nastavené tokens

  tokens:
    - name: simple-config
      audience: simple-config-server
      mount_path: /var/run/secrets/workload-identity/simple-config # default
      path: token                                                  # default
      expiration_seconds: 3600                                     # default
```

Výsledný deployment obsahuje `serviceAccountName`, `automountServiceAccountToken: false`, projektovaný `serviceAccountToken` volume a read-only mount. Pokud je `service_account.create=true`, `kube-build-app` zároveň vygeneruje `ServiceAccount` manifest.

Na deklarovaný token lze odkázat z primárního kontejneru, sidecaru nebo
explicitního initContaineru bez opakování jeho výsledné cesty:

```yaml
sidecars:
  - name: simple-idm-token-proxy
    image: registry.example.test/simple-idm-token-proxy:1.0.0
    envs:
      - name: SIMPLE_IDM_TOKEN_PROXY_TOKEN_FILE
        workload_identity_token_ref_name: simple-config
```

Příklad vygeneruje
`SIMPLE_IDM_TOKEN_PROXY_TOKEN_FILE=/var/run/secrets/workload-identity/simple-config/token`.
Vlastní hodnoty `tokens[].mount_path` a `tokens[].path` se respektují.
`workload_identity_token_ref_name` je reference na `tokens[].name`;
neexistující reference nebo kombinace s jiným zdrojem hodnoty environment
proměnné je validační chyba. Přípona `_ref_name` záměrně zviditelňuje
symbolické reference metamodelu. Stejnou referenci lze zdědit přes
`apps/_defaults.yml` a `container_envs`.

Pro běžný Downward API mount s runtime informacemi o podu použijte `pod_info`:

```yaml
pod_info:
  enabled: true
  mount_path: /etc/podinfo
```

Vzniknou například soubory:

```text
/etc/podinfo/namespace
/etc/podinfo/pod_name
```

Pro vlastní Downward API mount použijte explicitní tvar:

```yaml
downward_api:
  mounts:
    - name: runtime-info
      mount_path: /etc/runtime-info
      items:
        - path: namespace
          field_path: metadata.namespace
        - path: pod_name
          field_path: metadata.name
```

`pod_info` a `downward_api.mounts` zpřístupňují metadata jako soubory. Pro
zpřístupnění pod metadata pomocí environment proměnných použijte
`envs[].field_path`. Znovupoužitelný opt-in profil udrží mapování na jednom
místě. Definujte ho v `apps/_defaults.yml`:

```yaml
container_profiles:
  - name: kubernetes-runtime-metadata
    defaults:
      envs:
        - name: POD_NAME
          field_path: metadata.name
        - name: POD_NAMESPACE
          field_path: metadata.namespace
        - name: POD_IP
          field_path: status.podIP
        - name: NODE_NAME
          field_path: spec.nodeName
```

Profil explicitně zapněte u každého containeru, který tyto proměnné potřebuje:

```yaml
containers:
  - name: "{{var:APP_NAME}}"
    image: "..."
    profile_ref_names:
      - kubernetes-runtime-metadata
```

Vygenerovaný container dostane `POD_NAME`, `POD_NAMESPACE`, `POD_IP` a
`NODE_NAME` přes Kubernetes `valueFrom.fieldRef`. Proměnné pouze zpřístupní
metadata procesu; jejich přidání do metrik nebo strukturovaných logů musí
zajistit konfigurace aplikace nebo sběrné vrstvy.

Pod name je jen runtime/audit metadata. Autorizace má používat normalizovanou workload identitu `namespace/serviceAccount` z projektovaného tokenu.

### 16. Runtime Assets

`runtime_asset_definitions` definuje opakovaně použitelné skupiny
autorizovaných binárních nebo textových souborů. Appky a containery je vybírají
pomocí `runtime_asset_ref_names`. Vygenerovaný init container použije
projektovaný token deklarovaný ve `workload_identity`, stáhne soubory a uloží
je do sdíleného `emptyDir` volume ještě před startem aplikačních containerů.

Opakovaně použitelný asset definujte v `apps/_defaults.yml`:

```yaml
workload_identity:
  service_account:
    create: true
    automount: false
  tokens:
    - name: simple-config
      audience: simple-config-server

runtime_asset_defaults:
  source:
    type: simple_config
    base_url: https://config.example.test/simple-config-server
    tenant: default
    environment: test
    label: release-2026.07 # volitelný Git label
    workload_identity_token_ref_name: simple-config
    ca_shared_asset_ref_name: internal-ca
    timeout_seconds: 30
  volume:
    name: runtime-config
    mount_path: /app/runtime-config
    medium: Memory
    size_limit: 16Mi
  fetcher:
    image: registry.example.test/simple-idm-token-proxy:1.0.0
    # command má výchozí hodnotu simple-idm-token-proxy
    # image_pull_policy má výchozí hodnotu Always

runtime_asset_definitions:
  - name: java-runtime-config
    files:
      - source: files/ssl/tsm-client-keystore.jks
        target: tsm-client-keystore.jks
        mode: "0440"
        sha256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

      - source: files/ssl/tsm-client-truststore.jks
        target: tsm-client-truststore.jks
        mode: "0440"

  - name: connector-runtime-config
    files:
      - source: files/infrastructure/mtls-connector-worker.yaml
        target: mtls-connector-worker.yaml
        mode: "0440"
```

`runtime_asset_defaults` poskytne celé bloky `source`, `volume` a `fetcher`,
pokud je konkrétní definice vynechá. Definice může kterýkoli blok nahradit tím,
že jej sama uvede celý. Jednotlivá pole uvnitř bloku se neslučují, takže je
výsledná konfigurace jednoznačná.

Vyberte ho v appce pro všechny primární `containers[]`:

```yaml
runtime_asset_ref_names:
  - java-runtime-config
  - connector-runtime-config
```

Nebo jen pro jeden runtime container:

```yaml
containers:
  - name: api
    runtime_asset_ref_names:
      - java-runtime-config
```

`runtime_asset_ref_names` v sidecarech je také podporované pro vzácný případ,
kdy runtime volume potřebuje i sidecar. App-level refs se na sidecary nikdy
neaplikují.

```yaml
sidecars:
  - name: mtls-gateway
    runtime_asset_ref_names:
      - connector-runtime-config
```

Vybrané definice mohou sdílet volume, pokud mají shodné hodnoty `name`,
`mount_path`, `medium` a `size_limit`. Pod dostane jedno volume a každý
container nejvýše jeden odpovídající mount. Fetch init containery zůstávají
samostatné a běží postupně. Dvě definice nesmějí do sdíleného volume zapisovat
stejný `target`. Odlišně nakonfigurované volume se stejným jménem je také
validační chyba.

`source.workload_identity_token_ref_name` odkazuje na
`workload_identity.tokens[].name`; nevytváří druhý token ani audience.
Reference na neexistující token je validační chyba.

Opakovaně použitelným položkám v `<environment>/shared.assets.yml` přiřaďte
stabilní `name`. `source.ca_shared_asset_ref_name` toto jméno přeloží na
kanonickou cestu `to`. Vygenerovaný fetch init container odpovídající asset
připojí a předá jeho cestu fetcheru pomocí `--ca-file`. Fetcher přidá PEM bundle
ke svým běžným důvěryhodným kořenům; ověřování TLS zůstává zapnuté.

```yaml
# <environment>/shared.assets.yml
assets:
  - name: internal-ca
    file: assets/ssl/internal-ca.pem
    to: /var/run/certs/internal-ca.pem
```

Stejnou cestu lze bez jejího opakování exportovat do containeru:

```yaml
envs:
  - name: SSL_CERT_FILE
    shared_asset_ref_name: internal-ca
```

`source.ca_file` zůstává explicitní únikovou variantou pro nepojmenovaný shared
asset. Musí jít o absolutní cestu přesně odpovídající některé hodnotě `to`.
`source.ca_file` a `source.ca_shared_asset_ref_name` se vzájemně vylučují a ani
jedno nelze kombinovat se `source.insecure_upstream_tls`.

Výchozí hodnoty:

- `source.type`: `simple_config`
- `source.tenant`: `default`
- `source.environment`: prostředí předané přes `-e`
- `source.timeout_seconds`: `30`
- `volume.name`: název skupiny runtime assetů
- `files[].mode`: `"0440"`
- resources fetcheru: CPU `10m..100m`, memory `16Mi..128Mi`

Neexistující reference na runtime asset, token nebo shared asset je validační
chyba. Mount v aplikačním containeru je read-only; zapisovat do něj může pouze
vygenerovaný fetch init container.

Fetcher spouští:

```text
simple-idm-token-proxy fetch
```

Projektovaný token posílá přímo do `simple-config-server`. Localhost proxy
nepoužívá, protože běžné sidecary startují až po dokončení init containerů.
Dlouhodobě běžící aplikační klient může nezávisle používat
`simple-idm-token-proxy serve` jako běžný sidecar.

Runtime assety se stáhnou jednou při startu Podu; průběžně se nesynchronizují.
Změna vzdáleného souboru proto vyžaduje restart nebo rollout Podu. Pokud musí
být deployment svázaný s reprodukovatelnou Git revizí, použijte `source.label`.

### 17. Sidecars a Pod Options

`sidecar_definitions` v `apps/_defaults.yml` použijte pro opakovaně použitelné
pomocné containery. Appky si je explicitně vybírají pomocí `sidecar_ref_names`:

```yaml
# apps/_defaults.yml
workload_identity:
  service_account:
    create: true
  tokens:
    - name: simple-config
      audience: simple-config-server

sidecar_definitions:
  - name: simple-idm-token-proxy
    image: "{{TSM_REGISTRY_URL}}/simple-idm-token-proxy:{{TSM_RELEASE_ID}}"
    startup:
      command:
        - simple-idm-token-proxy
      arguments:
        - serve
        - --listen
        - 127.0.0.1:9999
        - --upstream
        - https://config.example.test/simple-config-server
        - --token-file
        - /var/run/secrets/workload-identity/simple-config/token
    resources:
      cpu:
        from: "10m"
        to: "100m"
      memory:
        from: "32Mi"
        to: "128Mi"

# apps/api.yml
sidecar_ref_names:
  - simple-idm-token-proxy

containers:
  - name: api
    image: "{{TSM_REGISTRY_URL}}/api:{{TSM_RELEASE_ID}}"
    envs:
      - name: SPRING_CLOUD_CONFIG_URI
        value: http://127.0.0.1:9999
```

Sidecary se renderují jako běžné Kubernetes containery ve stejném Podu. Síť v
Podu sdílí automaticky, takže `127.0.0.1` funguje mezi aplikačním containerem a
sidecarem. Workload identity token mounty i Downward API mounty se mountují i do
sidecarů.

Appka může vybraný znovupoužitelný sidecar upravit lokální položkou `sidecars`
se stejným `name`. Sidecar musí být dál uvedený v `sidecar_ref_names`; jinak se
patch odmítne migrační chybou. Shodné `envs` se mergují podle `name`, takže
app-specific override nevyžaduje opsat celý sidecar. Lokální `sidecars` položka
bez odpovídající definice se přidá jako sidecar jen pro danou appku.

Pro pomocné containery, které potřebují vidět procesy ostatních containerů ve stejném Podu, zapněte sdílený process namespace:

```yaml
pod:
  share_process_namespace: true

sidecar_ref_names:
  - cgroup-runtime-exporter

sidecars:
  - name: cgroup-runtime-exporter
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
```

Výsledkem je Kubernetes `shareProcessNamespace: true`. Porty sidecarů se nepoužívají pro generování Service; služby se generují jen z primárních `containers`.

### 18. Init Containers

App-level `init_containers` použijte pro obecné Kubernetes init containery:

```yaml
init_containers:
  - name: migrate
    image: registry.example.com/api-migrate:latest
    command: ["/bin/sh", "-c"]
    arguments: ["./migrate.sh"]
    envs:
      - name: LOG_LEVEL
        value: INFO
    env_from:
      - config_map: api-config
    mounts:
      - type: empty_dir
        name: work
        mount_path: /work
    security_context:
      runAsNonRoot: true
    resources:
      cpu:
        from: "50m"
        to: "100m"
      memory:
        from: "64Mi"
        to: "128Mi"
```

Podporovaná pole init containeru záměrně kopírují běžnou podmnožinu standardních containerů:

- `name`
- `image`
- `command`
- `arguments`
- `vars`
- `env_from`
- `mounts`
- `security_context`
- `resources`
- `raw`

Existující `tools` zůstávají preferovaná zkratka pro vystavení statických utilit přes generované init containery.

### 19. Raw Escape Hatches

`deployment_raw` použijte pro vzácná Deployment-level pole:

```yaml
deployment_raw:
  spec:
    revisionHistoryLimit: 2
```

`pod_raw` použijte pro vzácná pod spec pole:

```yaml
pod_raw:
  dnsPolicy: ClusterFirst
  enableServiceLinks: false
```

`deployment_raw` se rekurzivně merguje do vygenerovaného Deployment objektu. `pod_raw` se aplikuje do `spec.template.spec`.

### 20. Raw Container Fields

Raw passthrough používejte jen tehdy, když model nemá dedikované pole:

```yaml
containers:
  - name: api
    image: nginx:stable
    raw:
      securityContext:
        runAsNonRoot: true
```

Dedikovaná modelová pole jsou lepší, protože se dají validovat a zobrazit v UI nástrojích.

### 21. Cgroup Exporter Defaults

Na úrovni containeru lze zapnout automatické vkládání variable pro cgroup exporter:

```yaml
containers:
  - name: api
    image: api:latest
    enable_cgroup_exporter: true
```

Vkládají se jen chybějící proměnné:

```text
CGROUP_EXPORTER_METRICS_PREFIX
CGROUP_EXPORTER_METRICS_STATIC_LABELS
CGROUP_EXPORTER_LISTEN
CGROUP_EXPORTER_CPU_REQUESTS_MCPU
CGROUP_EXPORTER_CPU_LIMITS_MCPU
CGROUP_EXPORTER_MEMORY_REQUESTS_MIB
CGROUP_EXPORTER_MEMORY_LIMITS_MIB
CGROUP_EXPORTER_NODE_NAME
```

### 22. Ignorované Appky

Vynechání appky z buildu:

```yaml
ignore: true
name: experimental-api
```

Pro ignorované appky se negeneruje deployment, service ani assety.

## App Defaults

`apps/_defaults.yml` se merguje do každého app modelu v daném prostředí.

Příklad:

```yaml
tools:
  - name: util-apply-env
    image: registry.example.com/tools/apply-env:latest
    expose_bin: /usr/bin/apply-env
    mount_path: /usr/local/bin/apply-env

vars:
  - name: LOG_LEVEL
    value: INFO

container_envs:
  - container_ref_name: "*"
    envs:
      - name: GLOBAL_FLAG
        value: "true"

  - container_ref_name: api
    envs:
      - name: JAVA_OPTS
        value: "-Xms256m"

container_profiles:
  - name: java-jib-service
    defaults:
      image: "<from release manifest>"
      startup:
        command: ["/bin/sh"]
        arguments:
          - /app/start-java.sh
          - /app/jib-classpath-file
          - /app/jib-main-class-file
      envs:
        - name: SPRING_CONFIG_IMPORT
          value: "configserver:http://127.0.0.1:9999"
      probes:
        http:
          path: /actuator/health
          port: "{{env:DEFAULT_EXPOSE_PORT}}"

sidecar_definitions:
  - name: cgroup-runtime-exporter
    image: "{{env:DOCKER_HUB_URL}}/datalite/cgroup-runtime-exporter:2026.07.28.3"
    envs:
      - name: CGROUP_EXPORTER_TARGET_PID_REGEXP
        value: '(^|/)java(\s|$)'
```

Semantika:

- obecné map klíče se rekurzivně mergují, app hodnoty vítězí
- `vars` se párují podle `name`; app položka plně nahradí default položku
- `container_profiles` definují znovupoužitelné container defaults podle `name`
- `containers[].profile_ref_names` vybere jeden nebo více profilů pro daný container
- `sidecar_definitions` definují znovupoužitelné sidecary podle `name`
- `sidecar_ref_names` vybere jednu nebo více sidecar definic pro danou appku
- lokální `sidecars` může upravit vybraný sidecar podle `name` nebo přidat sidecar jen pro appku
- profily se mergují v uvedeném pořadí, potom vítězí lokální hodnoty containeru
- `container_profiles[].defaults.name` není povolené; názvy containerů patří do app souborů
- `container_profiles[].defaults.envs` a lokální `containers[].envs` se párují podle `name`
- `container_envs` se aplikují podle `container_ref_name`
- `container_ref_name: "*"` se aplikuje na všechny containery jako první
- concrete container defaults se aplikují potom
- lokální `containers[].envs` se aplikují poslední
- shodné variables se plně nahrazují podle `name`

Mazání / tombstone:

```yaml
vars:
  - name: LOG_LEVEL
    remove: true
```

```yaml
containers:
  - name: api
    envs:
      - name: GLOBAL_FLAG
        remove: true
```

`remove: true` položku úplně odstraní a nesmí být kombinováno s dalšími datovými poli.

## Replica Profily

Env-level profily umožní změnit repliky bez editace app souborů:

`<environment>/replica-profiles.yml`:

```yaml
defaults:
  replica_profile_ref_name: normal

profiles:
  normal:
    apps:
      api: 2
      worker: 1

  maintenance:
    all: 0
    apps:
      api: 1
```

Spuštění:

```bash
kube-build-app build -e test -p maintenance -R environments -t deploy/test
```

Přímé stažení vybraných appek na nulu:

```bash
kube-build-app build -e test -w worker -R environments -t deploy/test
```

## Rollout Checksums

Rollout checksum anotace použijte, když se má pod restartovat po změně vybraných souborů.

```yaml
rollout_on:
  checksums:
    config:
      files:
        - env.unsecured.json
        - env.secured.json
    mtls:
      files:
        - assets/infrastructure/mtls-gateway-config.tpl
```

Výsledný deployment pod template obsahuje:

```yaml
spec:
  template:
    metadata:
      annotations:
        checksum/config: "..."
        checksum/mtls: "..."
```

Chování:

- cesty jsou relativní k `<environment_dir>`
- soubory se před hashováním seřadí
- checksum zahrnuje relativní cestu a obsah souboru
- chybějící soubor failne build
- změna anotace změní pod template a vyvolá rollout

Používejte to jen pro deterministické deklarativní soubory. Ne pro hodnoty injektované později sidecarem nebo čistě runtime mechanismem.

## Inventory

Inventory vypíše strukturovaný JSON pohled na to, co se bude buildit:

```bash
kube-build-app inventory -e test -R environments
```

Legacy ekvivalent:

```bash
kube-build-app -i -e test -R environments
```

Je určené pro automatizaci, UI a generátory typu PKI helperů.

## Validace

Validace modelu bez zápisu manifestů:

```bash
kube-build-app validate -e test -R environments
```

Validace chytá známé nevalidní kombinace, například konflikt startup/simple-init nebo nevalidní tombstones.

## Helm-Safe Assets

Pokud se vygenerované manifesty renderují ještě Helmem, zbývající holé placeholdery v textových assetech mohou kolidovat s Helm syntaxí.

Použijte:

```bash
kube-build-app build -e test -R environments -t deploy/test --helm-escape-assets
```

Zbývající placeholdery se přepíšou z:

```text
{{VAR}}
```

na:

```text
{{`{{VAR}}`}}
```

Pokud má asset `transform: true`, nejdřív se aplikují build-time proměnné a až potom Helm escaping zbývajících placeholderů.

## mTLS Assets

Při container-level `mtls.enabled: true` `kube-build-app` automaticky mountuje šifrované mTLS soubory do containeru.

Inventory obsahuje očekávané cesty, aby externí tooling mohl vytvořit odpovídající secured materiál.

## Docker

Build image:

```bash
docker build -t kube-build-app:latest .
```

Run:

```bash
docker run --rm \
  -v "$PWD/environments:/work/environments:ro" \
  -v "$PWD/deploy:/work/deploy" \
  kube-build-app:latest \
  kube-build-app build -e test -R /work/environments -t /work/deploy/test
```

## Vývoj

Testy:

```bash
just test
```

Lokální binárky:

```bash
just build
```

Cross-platform build:

```bash
just build-cross
just build-cross-all
```

Generované manifesty používají výchozí odsazení YAML dvěma mezerami, stejně jako Ruby implementace. Pokud repozitář výslovně vyžaduje čtyři mezery, použijte `--yaml-indent 4`:

```bash
kube-build-app build -e test -R environments -t deploy/test --yaml-indent 4
```

`just build-cross` vloží do každé binárky `kube-build-app` hodnotu `VERSION`, Git commit a čas buildu v UTC. Artefakt ověříte pomocí `kube-build-app --version`.

Výstup verze je jeden textový řádek: `kube-build-app version <verze> (commit <commit>, built <čas v UTC>)`. Spuštění ze zdrojů bez build metadat vypíše `dev` a `unknown`.

Release binárky s version metadata a SHA256 checksumy:

```bash
just release 0.1.0
```

README smoke fixture:

```bash
just readme-smoke
```

Fixture je v `fixtures/readme-smoke/environments/dev` a ověřuje dokumentovaný CLI flow nad malým environment repozitářem:

```bash
kube-build-app validate -e dev -R fixtures/readme-smoke/environments
kube-build-app list -e dev -R fixtures/readme-smoke/environments
kube-build-app summary -e dev -R fixtures/readme-smoke/environments
kube-build-app inventory -e dev -R fixtures/readme-smoke/environments
kube-build-app build -e dev -R fixtures/readme-smoke/environments -t /tmp/kube-build-app-readme-smoke
```

Parity proti Ruby referenci:

```bash
scripts/parity-build \
  --name cetin-test \
  --root /path/to/tsm-environments \
  --env test \
  --release-id 2025.08.18.1 \
  --go-bin ./dist/kube-build-app
```

Reprezentativní smoke parity cases:

```bash
just parity-smoke
```

## GitLab Release Pipeline

GitLab pipeline používá soubor `VERSION` jako release trigger.

Při změně `VERSION` na default branch se spustí:

- vygenerování release notes z git historie
- cross-platform build `kube-build-app`
- vytvoření `tar.gz` balíčků
- vygenerování `SHA256SUMS`
- vytvoření nebo aktualizace GitLab Release
- upload do GitLab Generic Package Registry

Názvy release balíčků:

```text
kube-build-app-<version>-darwin-arm64.tar.gz
kube-build-app-<version>-darwin-amd64.tar.gz
kube-build-app-<version>-linux-amd64.tar.gz
kube-build-app-<version>-linux-arm64.tar.gz
kube-build-app-<version>-windows-amd64.tar.gz
SHA256SUMS
```

Každý platformní balíček obsahuje obě binárky:

```text
kube-build-app
kube-edit-app
```

Volitelný push `CHANGELOG.md`:

```text
RELEASE_PUSH_TOKEN
```

Pokud je `RELEASE_PUSH_TOKEN` nastavený v GitLab CI/CD variables, publish job aktualizuje `CHANGELOG.md` a pushne změnu zpět s `[skip ci]`.

## GitHub Release Workflow

GitHub Actions workflow provede stejný release build při změně `VERSION` ve
větvi `main`. Push do remote `github` následně:

- spustí kompletní Go testy
- vygeneruje release notes z git historie
- vytvoří stejné multiplatformní balíčky jako GitLab
- vytvoří nebo aktualizuje GitHub Release `v<VERSION>`
- nahraje všechny `tar.gz` balíčky a `SHA256SUMS`

Workflow používá `GITHUB_TOKEN` poskytovaný repozitářem; žádný další release
token není potřeba. Commity beze změny `VERSION` tento workflow nespustí.

## Licence

Tento projekt je poskytován pod licencí MIT.

Podrobnosti jsou uvedeny v souboru [LICENSE](LICENSE).

## Podpora

Tento repozitář je poskytován tak, jak je, a není provozován jako komunitně podporovaný projekt. GitHub Issues ani bezplatná komunitní podpora nejsou poskytovány.

Komerční podpora, včetně technické pomoci, ověřených releasů, oprav chyb, aktualizací a dlouhodobé údržby, je dostupná od společnosti [DataLite, spol. s r.o.](https://datalite.cz/).

Přijetí pull requestu je zcela na rozhodnutí správce projektu. Správce nemá povinnost pull request posoudit, přijmout ani na něj reagovat.
