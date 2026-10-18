# report-service — эталонный сервис на Java

Небольшой HTTP-сервис: принимает **XML**, берёт **XSLT-шаблон** по имени, делает из него HTML и отдаёт
**PDF** через Flying Saucer 9.1.22 + iText 5.5.12 — теми же библиотеками, что и в боевом контуре.
Нужен как эталон для сверки с Go-движком (`internal/xhr`) и как самостоятельный сервис.

## Сборка и запуск

Нужны JDK 17+ и Maven.

```sh
cd java-service
mvn -B package                      # target/report-service.jar (≈ 8 МБ, всё внутри)
TEMPLATES_DIR=/путь/к/templates FONTS_DIR=/путь/к/fonts PORT=8080 \
  java -Djava.awt.headless=true -jar target/report-service.jar
```

Docker:

```sh
docker build -t report-service java-service
docker run -p 8080:8080 -v $PWD/templates:/app/templates -v $PWD/fonts:/app/fonts report-service
```

| Переменная | По умолчанию | Смысл |
|---|---|---|
| `PORT` | `8080` | порт |
| `TEMPLATES_DIR` | `./templates` | каталог с шаблонами (`*.xslt` и подключаемые `xsl:import`) |
| `FONTS_DIR` | `./fonts` | шрифты: `arial.ttf`, `arialbd.ttf`, `ariali.ttf`, `arialbi.ttf`; если их нет — `LiberationSans-Regular/Bold/Italic/BoldItalic.ttf` |
| `MAX_BODY_MB` | `50` | максимальный размер XML |
| `THREADS` | `4` | число параллельных запросов |

## API

```sh
# PDF
curl -X POST 'http://localhost:8080/pdf?template=IndividualAdvancedReport.xslt&root=Root' \
     --data-binary @report.xml -o report.pdf

# HTML, который получает рендерер (для отладки шаблонов)
curl -X POST '...&format=html' --data-binary @report.xml

curl http://localhost:8080/health
```

* `template` — имя файла внутри `TEMPLATES_DIR` (выход за каталог запрещён);
* `root` — необязательно: сделать первый элемент с таким именем корнем документа (шаблоны обращаются к `/Root/...`);
* ошибки: `400` (нет шаблона, неверный XML), `413` (слишком большое тело), `500` (сбой рендера, текст в теле).

## Что делает

1. Разбирает XML (DOCTYPE и внешние сущности запрещены).
2. Заменяет `PATH_TO_FILE` в шаблоне на каталог шаблона (для `xsl:import`/`xsl:include`), применяет XSLT (JDK, XSLTC);
   шаблоны компилируются один раз и кэшируются, пока файл не изменился.
3. Сериализует результат в XML с отступами по правилам боевого контура (элементы с текстом пишутся как есть) и
   отдаёт строку Flying Saucer.
4. Регистрирует семейство `Arial` из `FONTS_DIR`; `<img src="data:image/svg+xml;base64,…">` растеризуется Batik
   (Flying Saucer сам SVG не рисует) и вставляется как битмап.

## Сверка с Go-версией

```sh
# Go
fop-go -xml report.xml -xmlroot Root -xsl templates/IndividualAdvancedReport.xslt -pdf go.pdf
# Java
curl -X POST '…?template=IndividualAdvancedReport.xslt&root=Root' --data-binary @report.xml -o java.pdf
# сравнение позиций текста
go run ./tools/pdfcmp words go.pdf java.pdf
```

Шаблоны, использующие `display: flex` в `@page`-верхнем колонтитуле, стоковый Flying Saucer не поддерживает —
flex-раскладку Go-движок эмулирует, поэтому для честного сравнения замените её на `display: block`
или сравнивайте остальные страницы.

## Лицензии

Сервис использует iText 5 (**AGPL-3.0**), Flying Saucer (LGPL-2.1+) и Batik (Apache-2.0). Собранный jar содержит
iText: если вы отдаёте сервис третьим лицам (в том числе как сетевой сервис), условия AGPL распространяются и на него.
Для внутреннего использования ограничений нет. Исходный код самого сервиса — LGPL-3.0-or-later, как и весь репозиторий.
