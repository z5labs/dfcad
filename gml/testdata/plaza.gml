<?xml version="1.0" encoding="UTF-8"?>
<riverside:FeatureCollection xmlns:riverside="https://example.org/models/riverside" xmlns:gml="http://www.opengis.net/gml/3.2" gml:id="riverside">
  <gml:boundedBy>
    <gml:Envelope srsName="EPSG:6543" srsDimension="2">
      <gml:lowerCorner>3502100.5 552000.25</gml:lowerCorner>
      <gml:upperCorner>3502140.5 552024.25</gml:upperCorner>
    </gml:Envelope>
  </gml:boundedBy>
  <gml:featureMember>
    <riverside:region gml:id="site.P-01">
      <riverside:id>site:P-01</riverside:id>
      <riverside:label>Plot one, &#34;the yard&#34; &amp; &lt;the shed&gt;</riverside:label>
      <riverside:kind>Site</riverside:kind>
      <riverside:geometry>
        <gml:MultiSurface gml:id="site.P-01.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:surfaceMember>
            <gml:Polygon gml:id="site.P-01.surface.1">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502100.5 552000.25 3502140.5 552000.25 3502140.5 552024.25 3502100.5 552024.25 3502100.5 552000.25</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
              <gml:interior>
                <gml:LinearRing>
                  <gml:posList>3502114.5 552008.25 3502126.5 552008.25 3502126.5 552016.25 3502114.5 552016.25 3502114.5 552008.25</gml:posList>
                </gml:LinearRing>
              </gml:interior>
            </gml:Polygon>
          </gml:surfaceMember>
        </gml:MultiSurface>
      </riverside:geometry>
    </riverside:region>
  </gml:featureMember>
  <gml:featureMember>
    <riverside:region gml:id="site.S-101">
      <riverside:id>site:S-101</riverside:id>
      <riverside:label>Meeting Room A</riverside:label>
      <riverside:kind>Space</riverside:kind>
      <riverside:geometry>
        <gml:MultiSurface gml:id="site.S-101.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:surfaceMember>
            <gml:Polygon gml:id="site.S-101.surface.1">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502104.5 552004.25 3502108.5 552004.25 3502108.5 552007.25 3502104.5 552007.25 3502104.5 552004.25</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
            </gml:Polygon>
          </gml:surfaceMember>
          <gml:surfaceMember>
            <gml:Polygon gml:id="site.S-101.surface.2">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502130.5 552004.25 3502134.5 552004.25 3502134.5 552007.25 3502130.5 552007.25 3502130.5 552004.25</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
            </gml:Polygon>
          </gml:surfaceMember>
        </gml:MultiSurface>
      </riverside:geometry>
    </riverside:region>
  </gml:featureMember>
  <gml:featureMember>
    <riverside:region gml:id="site.PNL-01">
      <riverside:id>site:PNL-01</riverside:id>
      <riverside:label>Distribution panel 1</riverside:label>
      <riverside:kind>Element</riverside:kind>
      <riverside:geometry>
        <gml:MultiPoint gml:id="site.PNL-01.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:pointMember>
            <gml:Point gml:id="site.PNL-01.point.1">
              <gml:pos>3502106.5 552005.25</gml:pos>
            </gml:Point>
          </gml:pointMember>
        </gml:MultiPoint>
      </riverside:geometry>
    </riverside:region>
  </gml:featureMember>
</riverside:FeatureCollection>
