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
- PDF: метаданные, Flate-сжатие, закладки, аннотации-ссылки.

## Ограничения (чего пока нет)

- Автоматическая раскладка таблиц по содержимому (`table-layout="auto"`): колонки без ширины
  делят доступное место поровну (как и в FOP).
- Сносы слов по словарю (hyphenation), RTL/BiDi, сложное шейпирование (лигатуры, арабский,
  индийские письменности), кернинг.
- `fo:footnote` (рендерится только ссылка), `fo:float`, `instream-foreign-object`/SVG,
  `multi-switch` и другие интерактивные объекты.
- Шифрование, PDF/A, PDF/UA (теги), вывод в форматы кроме PDF.
- Только горизонтальное письмо слева направо (`lr-tb`).

## Структура

| Пакет | Назначение |
|---|---|
| `xmldom` | DOM с пространствами имён, парсер (UTF-8, windows-1251, KOI8-R, ISO-8859-1/5), сериализатор |
| `xpath` | XPath 1.0 + паттерны XSLT |
| `xslt` | компилятор и исполнитель XSLT |
| `internal/font` | TTF: cmap, метрики, подмножество; менеджер шрифтов; встроенные Liberation |
| `internal/pdf` | запись PDF 1.7 |
| `internal/fo` | свойства FO, вёрстка, пагинация, рендер |
| `cmd/fop-go` | командная строка |

Примеры: `examples/invoice` (XML+XSLT → счёт) и `examples/features` (многостраничный FO:
оглавление с лидерами, таблица на несколько страниц, колонки, изображения, маркеры).

## Лицензии

Встроенные шрифты Liberation распространяются по SIL Open Font License 1.1
(см. `internal/font/data/LICENSE-Liberation.txt`).
