# ru-geo-lists

Простые текстовые списки доменов и CIDR для роутеров и прокси-клиентов, которые
не умеют читать `geosite.dat`/`geoip.dat`. Списки раз в сутки автоматически
генерируются из файлов [runetfreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat)
и публикуются в каталоге [`lists/`](lists/). Аналог по формату:
[itdoginfo/allow-domains](https://github.com/itdoginfo/allow-domains).

## Файлы и прямые ссылки

Базовый URL: `https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/`

| Файл | Содержимое | Источник |
|---|---|---|
| [ru.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/ru.txt) | домены + IPv4 + IPv6 | `geosite:category-ru` + `dion.vc`, `inno.tech`, `inno.local` + `geoip:ru` |
| [ru-domains.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/ru-domains.txt) | только домены | то же |
| [ru-ipv4.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/ru-ipv4.txt) | только IPv4-CIDR | `geoip:ru` |
| [ru-ipv6.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/ru-ipv6.txt) | только IPv6-CIDR | `geoip:ru` |
| [telegram.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/telegram.txt) | домены + IPv4 + IPv6 | `geosite:telegram` + `geoip:telegram` |
| [telegram-domains.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/telegram-domains.txt) | только домены | `geosite:telegram` |
| [telegram-ipv4.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/telegram-ipv4.txt) | только IPv4-CIDR | `geoip:telegram` |
| [telegram-ipv6.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/telegram-ipv6.txt) | только IPv6-CIDR | `geoip:telegram` |
| [youtube.txt](https://raw.githubusercontent.com/nihestver/ru-geo-lists/main/lists/youtube.txt) | только домены | `geosite:youtube` |

Три основных файла: `ru.txt`, `telegram.txt`, `youtube.txt`. Остальные —
производные для клиентов, которые не принимают смешанные списки или IPv6.
Пути опубликованных файлов после первого релиза не меняются.

## Формат

- Ровно один домен или один CIDR на строку. Никаких комментариев, заголовков,
  пустых строк и BOM.
- Переводы строк только LF, в конце файла есть перевод строки (закреплено
  в `.gitattributes`).
- Нижний регистр, без дубликатов, детерминированная сортировка: при
  неизменных исходных данных байты файла не меняются.
- В комбинированных файлах сначала домены (байтовый порядок), затем IPv4-CIDR,
  затем IPv6-CIDR (по адресу, потом по длине префикса).
- Домены без префиксов `domain:`, `full:`, `*.` и без ведущей точки, только
  `example.com`. Интернационализированные домены приведены к punycode
  (`xn--p1ai`).
- CIDR нормализованы (`10.0.0.0/8`, а не `10.1.2.3/8`), IPv6 в каноническом
  сокращённом виде.

Каждая строка с доменом означает «этот домен и все его поддомены» (как
`domain:` в v2ray или `DOMAIN-SUFFIX` в Clash).

## Семантика и известные ограничения

- `domain:` (RootDomain) и `full:` — оба становятся простой строкой `example.com`.
  **Записи `full:` в таком формате расширяются до суффиксного совпадения**:
  `full:www.example.com` в списке означает `www.example.com` и все его
  поддомены. Для этого формата это неизбежно.
- `keyword:` (Plain) и `regexp:` (Regex) в формат «один домен на строку» не
  выражаются: они пропускаются, их количество и примеры попадают в лог сборки
  и в сводку запуска workflow. В используемых сейчас категориях таких записей
  нет.
- Атрибуты доменов (`@cn`, `@ads` и т. п.) игнорируются, их количество
  логируется.
- Поддомены, уже покрытые родительским доменом из того же списка, удаляются.
  В `category-ru` есть записи верхнего уровня (`ru`, `su`, `xn--p1ai`,
  `moscow`, `yandex` и другие), поэтому все домены под ними в файл не попадают —
  они и так покрыты. Однометочные записи (TLD) выводятся в сводке отдельной
  строкой.
- Домены из `.dat`, чей TLD не числится в секции ICANN
  [Public Suffix List](https://publicsuffix.org/), пропускаются и считаются.
  Дополнительные домены из конфига (`inno.local`) эту проверку обходят.
- Для `geoip` учитывается флаг `inverse_match` (в старых схемах `reverse_match`):
  публикуется дополнение набора адресов в пределах семейства (IPv4 и IPv6
  отдельно), и в лог пишется предупреждение. В текущих данных флаг не
  установлен.
- Смежные и вложенные CIDR агрегируются (`10.0.0.0/25` + `10.0.0.128/25` →
  `10.0.0.0/24`). Агрегация никогда не меняет множество адресов; это проверяется
  тестом полным перебором адресного пространства. Сейчас upstream уже отдаёт
  агрегированные данные, поэтому число префиксов совпадает с исходным.
  Отключается в `config.yaml` (`aggregate_cidrs: false`).

## Расписание и автоматизация

Workflow [`update.yml`](.github/workflows/update.yml) запускается ежедневно в
**04:20 по Москве** (`cron: '20 4 * * *'` с `timezone: Europe/Moscow` — GitHub
Actions поддерживает IANA-зоны в `schedule`) и вручную через `workflow_dispatch`.

Каждый запуск:

1. Прогоняет `go vet` и тесты, собирает инструмент.
2. Скачивает `geosite.dat` и `geoip.dat` с повторными попытками, проверяет
   минимальный размер и `sha256` по опубликованным `*.sha256sum`.
3. Собирает списки и сравнивает их с уже опубликованными.
4. Проверяет результат валидатором.
5. Коммитит и пушит **только если файлы реально изменились**. Автор коммита —
   `github-actions[bot]`, в теле коммита статистика: строки, домены и CIDR по
   каждому файлу, прирост и убыль. Та же статистика попадает в
   `$GITHUB_STEP_SUMMARY`.

Защита от порчи публикации. Workflow падает и ничего не коммитит, если:
скачивание не удалось, файл подозрительно мал или не совпала контрольная сумма,
нужной категории нет или она пуста (в ошибке перечисляются похожие коды), либо
число строк любого файла изменилось больше чем на **30 %** относительно
предыдущей версии (порог `safety.max_change_ratio`). Изменения до 10 строк
(`safety.small_change_lines`) принимаются всегда, чтобы маленькие списки не
блокировались парой записей. Если большое изменение ожидаемо (например,
добавили категорию), запустите workflow вручную с флажком
`allow_large_change`.

Права workflow минимальны: на уровне workflow `permissions: {}`, у единственной
задачи `contents: write`. Параллельные запуски исключены через `concurrency`,
задан `timeout-minutes`, действия закреплены по SHA полного коммита.

### Поддержание активности

GitHub автоматически отключает scheduled-workflow в публичном репозитории, если
в нём 60 дней не было активности (см.
[документацию](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/disable-and-enable-workflows)).
Списки могут долго не меняться, и без коммитов расписание тихо отключится.
Механизм здесь минимальный и честный: если списки не изменились, а последнему
коммиту уже **50 дней** (`KEEPALIVE_DAYS` в workflow), задача делает один
коммит, обновляющий [`last-verified.txt`](last-verified.txt) — дату последней
успешной сверки с upstream и контрольные суммы `.dat`-файлов. Этот же файл
обновляется в каждом обычном коммите со списками. Никаких пустых коммитов и
запросов к API ради «активности» нет. Если workflow всё же отключится
(GitHub присылает письмо), его включают одной кнопкой на вкладке Actions или
командой `gh workflow enable update.yml`.

## Почему `.txt`

Проверено на `raw.githubusercontent.com`: и для `.lst`, и для `.txt` (и даже
для файлов без расширения) сервер отдаёт одинаковые заголовки —
`content-type: text/plain; charset=utf-8`, `x-content-type-options: nosniff`,
`access-control-allow-origin: *`, `cache-control: max-age=300`. Тип содержимого
определяется по содержимому, а не по расширению, поэтому для клиентов,
загружающих списки по URL, разницы нет. При равенстве выбран `.txt`: он
открывается штатным редактором на любой ОС без дополнительных ассоциаций,
понятен любому пользователю, и его используют многие источники списков
(например, `russia-mobile-internet-whitelist`); `.lst` распространён в
сообществе (itdoginfo/allow-domains, antifilter.download), но преимуществ не
даёт. Расширение задаётся одной константой `output.extension` в
`config.yaml`; после первого релиза его менять не следует, потому что клиенты
ссылаются на опубликованные пути.

## Конфигурация

Всё задаётся декларативно в [`config.yaml`](config.yaml): источники, каталог и
расширение выходных файлов, порог безопасности и сами списки. Добавить
категорию или домен — правка одной строки:

```yaml
lists:
  - name: ru
    geosite: [category-ru]           # коды категорий, регистр не важен
    geoip: [ru]
    extra_domains: [dion.vc, inno.tech, inno.local]
    outputs: [combined, domains, ipv4, ipv6]
```

Виды выходных файлов: `combined` → `<name>.txt`, `domains` → `<name>-domains.txt`,
`ipv4` → `<name>-ipv4.txt`, `ipv6` → `<name>-ipv6.txt`.

## Инструмент

`geo2list` — один небольшой бинарник на Go (зависимости: `protobuf/encoding/protowire`
для разбора wire-формата без сгенерированного кода, `x/net` для IDNA и Public
Suffix List, `yaml`). Схема сверена с
[`app/router/routercommon/common.proto`](https://github.com/v2fly/v2ray-core/blob/master/app/router/routercommon/common.proto)
v2ray-core.

```sh
go build -o geo2list ./cmd/geo2list

./geo2list fetch -dir .cache                       # скачать и проверить .dat
./geo2list inspect -geosite .cache/geosite.dat \
                   -geoip .cache/geoip.dat ru      # какие категории есть
./geo2list build -geosite .cache/geosite.dat \
                 -geoip .cache/geoip.dat           # собрать lists/
./geo2list validate                                # проверить формат lists/
go test ./...
```

`build` принимает `-dry-run`, `-allow-large-change`, `-summary файл.md`
и `-commit-message файл.txt`. Валидатор проверяет каждую строку (валидный
домен или CIDR в канонической форме), отсутствие пустых строк, дубликатов, CR
и BOM, порядок секций и сортировку; для производных файлов — что в них только
домены, только IPv4 или только IPv6.

## Источники, лицензии и атрибуция

Данные берутся из
[runetfreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat)
(ветка `release`, лицензия репозитория GPL-3.0), который собирает файлы из:

- [v2fly/domain-list-community](https://github.com/v2fly/domain-list-community)
  (MIT) — категории `category-ru`, `telegram`, `youtube` в `geosite.dat`
  (через [runetfreedom/russia-blocked-geosite](https://github.com/runetfreedom/russia-blocked-geosite), GPL-3.0);
- [Loyalsoldier/geoip](https://github.com/Loyalsoldier/geoip) (CC BY-SA 4.0) —
  страновые категории `geoip.dat`, включая `geoip:ru`
  (через [runetfreedom/russia-blocked-geoip](https://github.com/runetfreedom/russia-blocked-geoip), GPL-3.0).
  Страновые данные основаны на MaxMind GeoLite2:
  *This product includes GeoLite2 data created by MaxMind, available from
  [https://www.maxmind.com](https://www.maxmind.com).*
  Категория `geoip:telegram` формируется там же по ASN Telegram.

Списки в `lists/` — производные от этих данных и распространяются на условиях
соответствующих исходных лицензий (CC BY-SA 4.0 для данных GeoIP, MIT для
domain-list-community) с сохранением указанной атрибуции. Код инструмента —
[MIT](LICENSE).
