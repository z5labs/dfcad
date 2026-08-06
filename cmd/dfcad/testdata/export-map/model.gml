<?xml version="1.0" encoding="UTF-8"?>
<dfcad:FeatureCollection xmlns:dfcad="https://github.com/z5labs/dfcad/gml/1" xmlns:gml="http://www.opengis.net/gml/3.2" gml:id="model">
  <gml:boundedBy>
    <gml:Envelope srsName="EPSG:6543" srsDimension="2">
      <gml:lowerCorner>3502100 552000</gml:lowerCorner>
      <gml:upperCorner>3502140 552024</gml:upperCorner>
    </gml:Envelope>
  </gml:boundedBy>
  <gml:featureMember>
    <dfcad:region gml:id="region.1">
      <dfcad:id>site:P-01</dfcad:id>
      <dfcad:label>Plot one</dfcad:label>
      <dfcad:kind>Site</dfcad:kind>
      <dfcad:type>Parcel</dfcad:type>
      <dfcad:frame>frame:site-grid</dfcad:frame>
      <dfcad:geometry>
        <gml:MultiSurface gml:id="region.1.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:surfaceMember>
            <gml:Polygon gml:id="region.1.surface.1">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502100 552024 3502100 552000 3502140 552000 3502140 552024 3502100 552024</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
              <gml:interior>
                <gml:LinearRing>
                  <gml:posList>3502120 552020 3502128 552020 3502128 552014 3502120 552014 3502120 552020</gml:posList>
                </gml:LinearRing>
              </gml:interior>
            </gml:Polygon>
          </gml:surfaceMember>
        </gml:MultiSurface>
      </dfcad:geometry>
    </dfcad:region>
  </gml:featureMember>
  <gml:featureMember>
    <dfcad:region gml:id="region.2">
      <dfcad:id>site:S-101</dfcad:id>
      <dfcad:label>Meeting Room A</dfcad:label>
      <dfcad:kind>Space</dfcad:kind>
      <dfcad:type>MeetingRoom</dfcad:type>
      <dfcad:within>site:L-01</dfcad:within>
      <dfcad:frame>frame:building</dfcad:frame>
      <dfcad:geometry>
        <gml:MultiSurface gml:id="region.2.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:surfaceMember>
            <gml:Polygon gml:id="region.2.surface.1">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502104 552007 3502104 552004 3502108 552004 3502108 552007 3502104 552007</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
            </gml:Polygon>
          </gml:surfaceMember>
        </gml:MultiSurface>
      </dfcad:geometry>
    </dfcad:region>
  </gml:featureMember>
  <gml:featureMember>
    <dfcad:region gml:id="region.3">
      <dfcad:id>site:S-102</dfcad:id>
      <dfcad:label>Meeting Room B</dfcad:label>
      <dfcad:kind>Space</dfcad:kind>
      <dfcad:type>MeetingRoom</dfcad:type>
      <dfcad:within>site:L-01</dfcad:within>
      <dfcad:frame>frame:building</dfcad:frame>
      <dfcad:geometry>
        <gml:MultiSurface gml:id="region.3.geometry" srsName="EPSG:6543" srsDimension="2">
          <gml:surfaceMember>
            <gml:Polygon gml:id="region.3.surface.1">
              <gml:exterior>
                <gml:LinearRing>
                  <gml:posList>3502110 552008 3502110 552004 3502114 552004 3502115.1755705047 552004.3819660112 3502115.9021130325 552005.3819660112 3502115.9021130325 552006.6180339888 3502115.1755705047 552007.6180339888 3502114 552008 3502110 552008</gml:posList>
                </gml:LinearRing>
              </gml:exterior>
            </gml:Polygon>
          </gml:surfaceMember>
        </gml:MultiSurface>
      </dfcad:geometry>
    </dfcad:region>
  </gml:featureMember>
</dfcad:FeatureCollection>
