<?xml version="1.0" encoding="UTF-8"?>
<xsl:stylesheet version="1.0"
    xmlns:xsl="http://www.w3.org/1999/XSL/Transform"
    xmlns:fo="http://www.w3.org/1999/XSL/Format">

  <xsl:output method="xml" indent="yes"/>
  <xsl:param name="company-color" select="'#1a3a6e'"/>
  <xsl:decimal-format name="ru" decimal-separator="," grouping-separator=" "/>

  <xsl:template match="/invoice">
    <fo:root font-family="Arial, sans-serif" font-size="10pt">
      <fo:layout-master-set>
        <fo:simple-page-master master-name="page" page-width="210mm" page-height="297mm"
                               margin-top="15mm" margin-bottom="15mm" margin-left="20mm" margin-right="15mm">
          <fo:region-body margin-top="10mm" margin-bottom="12mm"/>
          <fo:region-before extent="10mm"/>
          <fo:region-after extent="10mm"/>
        </fo:simple-page-master>
      </fo:layout-master-set>

      <fo:declarations/>
      <fo:bookmark-tree>
        <fo:bookmark internal-destination="top"><fo:bookmark-title>Счёт № <xsl:value-of select="@number"/></fo:bookmark-title></fo:bookmark>
        <fo:bookmark internal-destination="items"><fo:bookmark-title>Позиции</fo:bookmark-title></fo:bookmark>
      </fo:bookmark-tree>

      <fo:page-sequence master-reference="page">
        <fo:title>Счёт № <xsl:value-of select="@number"/></fo:title>
        <fo:static-content flow-name="xsl-region-before">
          <fo:block font-size="8pt" color="#777777" text-align="right">
            <xsl:value-of select="seller/name"/> · ИНН <xsl:value-of select="seller/inn"/>
          </fo:block>
        </fo:static-content>
        <fo:static-content flow-name="xsl-region-after">
          <fo:block font-size="8pt" text-align="center" color="#777777">
            Страница <fo:page-number/> из <fo:page-number-citation-last ref-id="seq"/>
          </fo:block>
        </fo:static-content>

        <fo:flow flow-name="xsl-region-body" id="seq">
          <fo:block id="top" font-size="18pt" font-weight="bold" color="{$company-color}" space-after="2mm">
            Счёт на оплату № <xsl:value-of select="@number"/>
          </fo:block>
          <fo:block space-after="6mm" color="#444444">
            от <xsl:call-template name="date"><xsl:with-param name="d" select="@date"/></xsl:call-template>
          </fo:block>

          <fo:table table-layout="fixed" width="100%" space-after="6mm">
            <fo:table-column column-width="proportional-column-width(1)"/>
            <fo:table-column column-width="proportional-column-width(1)"/>
            <fo:table-body>
              <fo:table-row>
                <fo:table-cell padding-right="4mm">
                  <xsl:apply-templates select="seller"><xsl:with-param name="role" select="'Поставщик'"/></xsl:apply-templates>
                </fo:table-cell>
                <fo:table-cell>
                  <xsl:apply-templates select="buyer"><xsl:with-param name="role" select="'Покупатель'"/></xsl:apply-templates>
                </fo:table-cell>
              </fo:table-row>
            </fo:table-body>
          </fo:table>

          <fo:block id="items" font-weight="bold" space-after="2mm">Позиции счёта</fo:block>
          <fo:table table-layout="fixed" width="100%" border-collapse="collapse" font-size="9pt">
            <fo:table-column column-width="10mm"/>
            <fo:table-column column-width="proportional-column-width(1)"/>
            <fo:table-column column-width="17mm"/>
            <fo:table-column column-width="14mm"/>
            <fo:table-column column-width="25mm"/>
            <fo:table-column column-width="28mm"/>
            <fo:table-header>
              <fo:table-row background-color="{$company-color}" color="white" font-weight="bold">
                <xsl:call-template name="th"><xsl:with-param name="t" select="'№'"/></xsl:call-template>
                <xsl:call-template name="th"><xsl:with-param name="t" select="'Наименование'"/></xsl:call-template>
                <xsl:call-template name="th"><xsl:with-param name="t" select="'Кол-во'"/></xsl:call-template>
                <xsl:call-template name="th"><xsl:with-param name="t" select="'Ед.'"/></xsl:call-template>
                <xsl:call-template name="th"><xsl:with-param name="t" select="'Цена'"/></xsl:call-template>
                <xsl:call-template name="th"><xsl:with-param name="t" select="'Сумма'"/></xsl:call-template>
              </fo:table-row>
            </fo:table-header>
            <fo:table-body>
              <xsl:apply-templates select="items/item"/>
            </fo:table-body>
          </fo:table>

          <xsl:variable name="sum">
            <xsl:call-template name="sum-items"><xsl:with-param name="nodes" select="items/item"/></xsl:call-template>
          </xsl:variable>
          <xsl:variable name="vat" select="$sum * vat/@rate div (100 + vat/@rate)"/>

          <fo:block text-align="right" space-before="4mm" keep-together="always">
            <fo:block>Итого: <fo:inline font-weight="bold"><xsl:value-of select="format-number($sum, '# ##0,00', 'ru')"/> ₽</fo:inline></fo:block>
            <fo:block>В том числе НДС <xsl:value-of select="vat/@rate"/>%: <xsl:value-of select="format-number($vat, '# ##0,00', 'ru')"/> ₽</fo:block>
          </fo:block>

          <fo:block space-before="8mm" font-size="9pt" color="#555555" text-align="justify"><xsl:value-of select="note"/></fo:block>
        </fo:flow>
      </fo:page-sequence>
    </fo:root>
  </xsl:template>

  <xsl:template match="seller | buyer">
    <xsl:param name="role"/>
    <fo:block border="0.5pt solid #bbbbbb" padding="3mm" background-color="#f6f8fb">
      <fo:block font-size="8pt" color="#777777" text-transform="uppercase"><xsl:value-of select="$role"/></fo:block>
      <fo:block font-weight="bold" space-before="1mm"><xsl:value-of select="name"/></fo:block>
      <fo:block><xsl:value-of select="address"/></fo:block>
      <fo:block>ИНН: <xsl:value-of select="inn"/></fo:block>
    </fo:block>
  </xsl:template>

  <xsl:template match="item">
    <fo:table-row>
      <xsl:if test="position() mod 2 = 0"><xsl:attribute name="background-color">#f0f3f8</xsl:attribute></xsl:if>
      <xsl:call-template name="td"><xsl:with-param name="t" select="position()"/></xsl:call-template>
      <xsl:call-template name="td"><xsl:with-param name="t" select="title"/></xsl:call-template>
      <xsl:call-template name="td"><xsl:with-param name="t" select="qty"/><xsl:with-param name="a" select="'end'"/></xsl:call-template>
      <xsl:call-template name="td"><xsl:with-param name="t" select="unit"/></xsl:call-template>
      <xsl:call-template name="td"><xsl:with-param name="t" select="format-number(price, '# ##0,00', 'ru')"/><xsl:with-param name="a" select="'end'"/></xsl:call-template>
      <xsl:call-template name="td"><xsl:with-param name="t" select="format-number(qty * price, '# ##0,00', 'ru')"/><xsl:with-param name="a" select="'end'"/></xsl:call-template>
    </fo:table-row>
  </xsl:template>

  <xsl:template name="th">
    <xsl:param name="t"/>
    <fo:table-cell padding="2mm" border="0.5pt solid #1a3a6e"><fo:block><xsl:value-of select="$t"/></fo:block></fo:table-cell>
  </xsl:template>

  <xsl:template name="td">
    <xsl:param name="t"/>
    <xsl:param name="a" select="'start'"/>
    <fo:table-cell padding="2mm" border="0.5pt solid #bbbbbb"><fo:block text-align="{$a}"><xsl:value-of select="$t"/></fo:block></fo:table-cell>
  </xsl:template>

  <xsl:template name="sum-items">
    <xsl:param name="nodes"/>
    <xsl:param name="acc" select="0"/>
    <xsl:choose>
      <xsl:when test="count($nodes) = 0"><xsl:value-of select="$acc"/></xsl:when>
      <xsl:otherwise>
        <xsl:call-template name="sum-items">
          <xsl:with-param name="nodes" select="$nodes[position() &gt; 1]"/>
          <xsl:with-param name="acc" select="$acc + $nodes[1]/qty * $nodes[1]/price"/>
        </xsl:call-template>
      </xsl:otherwise>
    </xsl:choose>
  </xsl:template>

  <xsl:template name="date">
    <xsl:param name="d"/>
    <xsl:variable name="months" select="'января февраля марта апреля мая июня июля августа сентября октября ноября декабря'"/>
    <xsl:variable name="m" select="number(substring($d, 6, 2))"/>
    <xsl:value-of select="number(substring($d, 9, 2))"/>
    <xsl:text> </xsl:text>
    <xsl:call-template name="nth-word"><xsl:with-param name="s" select="$months"/><xsl:with-param name="n" select="$m"/></xsl:call-template>
    <xsl:text> </xsl:text>
    <xsl:value-of select="substring($d, 1, 4)"/>
  </xsl:template>

  <xsl:template name="nth-word">
    <xsl:param name="s"/><xsl:param name="n"/>
    <xsl:choose>
      <xsl:when test="$n = 1"><xsl:value-of select="substring-before(concat($s, ' '), ' ')"/></xsl:when>
      <xsl:otherwise>
        <xsl:call-template name="nth-word"><xsl:with-param name="s" select="substring-after($s, ' ')"/><xsl:with-param name="n" select="$n - 1"/></xsl:call-template>
      </xsl:otherwise>
    </xsl:choose>
  </xsl:template>
</xsl:stylesheet>
