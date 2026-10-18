# fop-go

Реализация конвейера **Apache FOP на чистом Go**: `XML + XSLT → XSL-FO → PDF`.
Без внешних зависимостей (только стандартная библиотека), без cgo, без Java.

```
data.xml ──┐
           ├─ XSLT 1.0 ─▶ XSL-FO ─▶ вёрстка ─▶ PDF
template.xsl ┘
```

Внутри всё написано с нуля: XML DOM, XPath 1.0, XSLT-процессор, разбор TrueType-шрифтов
с подмножеством для встраивания, движок вёрстки XSL-FO и PDF-писатель.

## Быстрый старт

CLI (по аналогии с `fop -xml … -xsl … -pdf …`):

```sh
go run ./cmd/fop-go -xml examples/invoice/invoice.xml -xsl examples/invoice/invoice.xsl -pdf invoice.pdf
go run ./cmd/fop-go -fo examples/features/features.fo -pdf features.pdf     # готовый XSL-FO
go run ./cmd/fop-go -xml d.xml -xsl t.xsl -pdf out.pdf -foout out.fo -param title=Отчёт
```

Библиотека:

```go
import fop "github.com/ProDGG/fop-go"

err := fop.RenderFiles("data.xml", "template.xsl", "out.pdf", fop.Options{
    Params: map[string]string{"company": "ООО «Ромашка»"},
    Warn:   func(s string) { log.Println(s) },
})

// или с потоками:
err = fop.Render(xmlReader, xslReader, pdfWriter, fop.Options{})
// только преобразование XML+XSLT → XSL-FO:
foTree, err := fop.Transform(xmlReader, xslReader, fop.Options{})
// готовый XSL-FO → PDF:
err = fop.RenderFO(foReader, pdfWriter, fop.Options{})
```

Параметры `Options`: `BaseDir` (для относительных путей картинок), `XSLBase` (для
`xsl:include`/`xsl:import`/`document()`), `Params` (xsl:param), `Fonts`/`FontDirs`
(дополнительные TTF), метаданные (`Title`, `Author`, …), `NoCompress`, `SourceResolution`
(dpi для `px`), `AllowRemote` (http(s)-картинки), `Warn`.

## Шрифты и кириллица

В бинарник встроены шрифты **Liberation** (SIL OFL; метрически совместимы с Arial/Helvetica,
Times и Courier; латиница, кириллица, греческий). Семейства `Helvetica`, `Arial`, `sans-serif`,
`Times`, `serif`, `Courier`, `monospace` и т. п. отображаются на них. Шрифты встраиваются в PDF
как CID TrueType-подмножества с `ToUnicode`, поэтому текст в PDF копируется и ищется.

Свои шрифты: `-font "Моя гарнитура=/path/font.ttf,bold,italic"` или `Options.Fonts`.
Системные каталоги шрифтов сканируются при обращении к неизвестному семейству и для подстановки
отсутствующих символов (отключается `-nosystemfonts`). Поддерживаются TrueType с контурами `glyf`
(`.ttf`); OpenType/CFF (`.otf`) не поддерживается.

## Что поддерживается

**XSLT 1.0**: шаблоны, режимы, приоритеты, `import`/`include`, `apply-templates`/`call-template`,
параметры и переменные (в т. ч. фрагменты результата как node-set), `for-each`, `choose`, `sort`,
`key()`, `document()`, `number`, `format-number`/`decimal-format`, `copy`/`copy-of`,
`element`/`attribute`, `attribute-set`, `namespace-alias`, `strip-space`, `message`,
упрощённые стилевые листы, часть функций XPath 2.0 (`upper-case`, `replace`, `matches`,
`ends-with`, `abs`, …) и `exsl:node-set`.

**XSL-FO**:
- страницы: `simple-page-master`, пять регионов, `page-sequence-master` (single/repeatable/
  alternatives с `first/last/rest`, чётность, blank), несколько колонок (`column-count`),
  `initial-page-number`, `force-page-count`, нумерация и форматы (`1`, `i`, `I`, `a`, `A`);
- блоки: `block`, `block-container` (абсолютное позиционирование, `reference-orientation`),
  `wrapper`, `inline` (фон, рамка, отступы, индексы), `basic-link`, `character`,
  `list-block`/`list-item` (`label-end()`, `body-start()`), `leader` (dots/rule/space/use-content);
- таблицы: фиксированные колонки (`proportional-column-width`, %, длины), `colspan`/`rowspan`,
  `border-collapse` (collapse/separate), повтор шапки и подвала на каждой странице, разрыв строк;
- вёрстка: выключка, `text-indent`, отступы/поля/рамки/фон, `space-before/after` со схлопыванием,
  `keep-together`, `keep-with-next/previous`, `orphans`/`widows`, `break-before/after`
  (page/column/odd-page/even-page), `white-space`, `text-transform`, `text-decoration`,
  `letter-spacing`, `line-height`, мягкие переносы, `display-align`;
- ссылки и навигация: `id`, `page-number`, `page-number-citation(-last)` (многопроходно),
  внутренние/внешние ссылки, `bookmark-tree` (оглавление PDF), `marker`/`retrieve-marker`;
- изображения: `external-graphic` (PNG с прозрачностью, JPEG, GIF; `scale-to-fit`, `content-width`,
  data-URI), фоновые изображения;
- `fo:footnote` и SVG (см. ниже); парное кернинг GPOS; точная целочисленная арифметика FOP
  (миллипункты);
- PDF: метаданные, Flate-сжатие, закладки, аннотации-ссылки.

## HTML-отчёты (Flying Saucer + iText)

Если результат XSLT — HTML-документ (корневой элемент `html`), PDF строится не FO-движком, а
портом конвейера **Flying Saucer 9.1.22 + iText 5.5** (`internal/xhr`), цель — вывод, совпадающий
с эталонным PDF, а не просто «похожий».

```
fop-go -xml report.xml -xmlroot Root -xsl templates/IndividualAdvancedReport.xslt -pdf report.pdf
fop-go ... -font "Arial=/path/arial.ttf" -font "Arial=/path/arialbd.ttf,bold"   # настоящий Arial
```

**Что реализовано:** CSS 2.1 (каскад UA < автор, `!important`, специфичность, `@page`, margin boxes,
`position: running()`, `counter(page|pages)`), блочная и строчная вёрстка (порт `BlockBoxing`,
`InlineBoxing`, `Breaker` с таблицами `BreakIterator` из JDK), float, таблицы (fixed/auto, collapsed
borders), позиционирование (`relative/absolute/fixed`, `z-index`, слои как в `Layer`), `overflow:
hidden`, постраничная разбивка, закладки (`h1..h6` из DOM, `<head><bookmarks>`), ссылки, PNG/JPEG/GIF,
SVG, минимальный однострочный `display:flex` (шапка отчётов).

**Как достигается совпадение с эталоном:**

* Метрики шрифтов как у iText: ascent/descent из `head`-bbox, целочисленные ширины глифов, округления
  `ITextTextRenderer`; базовые 14 шрифтов (Helvetica/Times/Courier) берут метрики и ширины из AFM iText.
* `Arial` по умолчанию — встроенный Liberation Sans с вертикальными метриками Arial (геометрия
  совпадает, форма букв чуть отличается). Для полного совпадения передайте настоящие `arial.ttf` /
  `arialbd.ttf` через `-font` / `Options.Fonts` (тогда подмена метрик не нужна).
* Числа в PDF форматируются как `ByteBuffer.formatDouble` iText, фоны вырезаются по clip как
  `java.awt.geom.Area`, порядок отрисовки (фон → float → текст → картинки → позиционированные слои) как в
  `Layer.paint`.
* SVG в `<img>` растеризуется в антиалиасный битмап интринсик-размера (как в эталонном развёртывании);
  логотип 150×40 совпадает попиксельно.
* Растровые картинки `data:` декодируются строго как `java.util.Base64.getDecoder()` — перенос строки
  внутри base64 (как у QR-кода в шаблонах `personal`) отбрасывает картинку, как делает Flying Saucer.
* Результат XSLT сериализуется с `indent="yes"` и разбирается заново — так возвращается
  пробельный текст между элементами, который влияет на вёрстку.

**Опции:** `-xmlroot Root` (`Options.SourceRoot`) делает `Root` корнем входного XML (шаблоны обращаются
к `/Root/...`); `PATH_TO_FILE` в `xsl:import` заменяется на каталог шаблона (`Options.PathPlaceholder`).

**Проверка:** тесты `internal/xhr` сравнивают позиции текста с выводом настоящего Flying Saucer на
синтетических фикстурах (`internal/xhr/testdata`, эталоны получены на оригинальной реализации).
`tools/xhrdev` рендерит HTML напрямую и может сохранить дамп дерева боксов
(`xhrdev in.html out.pdf boxes.txt`) для сравнения с Java.

**Известные отличия от эталонного PDF:** форма букв без настоящего Arial; блоки, версия шаблонов
которых в проде отличается от присланной; пробельные строки в margin-боксах не рисуются; нет
`-fs-table-paginate` (повтор `thead`), `border-radius`/`box-shadow` (во Flying Saucer 9.1.22 их тоже нет).

## Ограничения (чего пока нет)

- Автоматическая раскладка таблиц по содержимому (`table-layout="auto"`): колонки без ширины
  делят доступное место поровну (как и в FOP).
- Сносы слов по словарю (hyphenation), RTL/BiDi, сложное шейпирование (лигатуры, арабский,
  индийские письменности).
- `fo:footnote`: тела сносок размещаются внизу колонки с разделителем
  `xsl-footnote-separator`, но сноска не переносится по частям между страницами (FOP умеет);
  `fo:float`, `multi-switch` и другие интерактивные объекты; `fo:table-and-caption`
  (в самом FOP не реализован и отбрасывается).
- SVG (`instream-foreign-object`, `external-graphic` с `.svg`) — подмножество: фигуры, пути,
  линейные/радиальные градиенты, текст, трансформации, прозрачность, пунктир; без фильтров,
  масок, `clipPath`, паттернов и CSS-таблиц.
- Шифрование, PDF/A, PDF/UA (теги), вывод в форматы кроме PDF.
- Только горизонтальное письмо слева направо (`lr-tb`).

## Совместимость с Apache FOP

Движок вёрстки — переработка алгоритмов FOP 2.x (проверено сравнением с настоящим FOP 2.11):
- разбиение на строки — полный перенос Кнута–Пласса (`BreakingAlgorithm`, пороги 1/5/20,
  штрафы, демериты), элементы строк как в `TextLayoutManager`/`LineLayoutManager`;
- метрики шрифта по правилам FOP (OS/2 typo vs hhea, xHeight), интерлиньяж, сдвиг базовой
  линии, `space-before/after` со схлопыванием (`SpaceResolver`), отступы, keeps/orphans/widows,
  `force-page-count`, колонки, маркеры, сноски;
- повторяются и «ошибки» FOP: `keep-together` как шорткат запрещает перенос внутри строки,
  `text-transform="capitalize"` с off-by-one между текстовыми узлами, `empty-cells="hide"`,
  центрированные переносы, ширины блок-контейнеров как размеры содержимого и т. д.

На тестовых документах (`testdata/fidelity/*.fo`) совпадают разбиение на строки и страницы и
положение базовых линий (до 0.01pt); растровое отличие страниц — доли процента.
Инструменты на Go в `tools/` (Python не нужен): `go run ./tools/fidelity` (нужны Java и jar-файлы FOP
2.x в `$FOPLIBS`, poppler-utils) и `go run ./tools/pdfcmp {words|lines|baselines|texts|img} a.pdf b.pdf`
для сравнения двух PDF по словам, строкам, базовым линиям, позициям текста и пикселям. Таблицы
свойств `internal/fo/props_gen.go` пересоздаются командой `FOP_SRC=/path/to/xmlgraphics-fop go generate ./internal/fo`.

## Структура

| Пакет | Назначение |
|---|---|
| `xmldom` | DOM с пространствами имён, парсер (UTF-8, windows-1251, KOI8-R, ISO-8859-1/5), сериализатор |
| `xpath` | XPath 1.0 + паттерны XSLT |
| `xslt` | компилятор и исполнитель XSLT |
| `internal/font` | TTF: cmap, метрики, подмножество; менеджер шрифтов; встроенные Liberation |
| `internal/pdf` | запись PDF 1.7 |
| `internal/fo` | свойства FO, вёрстка, пагинация, рендер |
| `internal/xhr` | HTML/CSS-движок, совместимый с Flying Saucer + iText 5 |
| `internal/pdfscan` | разбор текстовых операций PDF (для сравнений и тестов) |
| `tools/xhrdev`, `tools/pdfcmp`, `tools/fidelity`, `tools/genprops` | отладка, сравнение PDF, сверка с FOP, генератор свойств |
| `cmd/fop-go` | командная строка |

Примеры: `examples/invoice` (XML+XSLT → счёт) и `examples/features` (многостраничный FO:
оглавление с лидерами, таблица на несколько страниц, колонки, изображения, маркеры).

## Лицензии

Встроенные шрифты Liberation распространяются по SIL Open Font License 1.1
(см. `internal/font/data/LICENSE-Liberation.txt`).
