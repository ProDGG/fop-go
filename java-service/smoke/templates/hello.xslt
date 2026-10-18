<xsl:stylesheet version="1.0" xmlns:xsl="http://www.w3.org/1999/XSL/Transform">
  <xsl:output method="html" encoding="utf-8" indent="yes"/>
  <xsl:template match="Root">
    <html><head><style>@page { size: A4 landscape; margin: 1cm } td { border: 1px solid #888; padding: 4px; font-family: Arial }</style></head>
    <body><table><xsl:for-each select="Item"><tr><td><xsl:value-of select="@name"/></td></tr></xsl:for-each></table></body></html>
  </xsl:template>
</xsl:stylesheet>
