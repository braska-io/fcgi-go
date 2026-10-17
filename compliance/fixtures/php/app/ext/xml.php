<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/xml');
header('Content-Type: text/plain; charset=UTF-8');

switch ($q['op'] ?? '') {
    case 'dom_roundtrip':
        $d = new DOMDocument();
        $d->loadXML('<root><a x="1">t</a><b/></root>');
        echo $d->saveXML();
        break;
    case 'dom_build':
        $d = new DOMDocument('1.0', 'UTF-8');
        $root = $d->appendChild($d->createElement('root'));
        $item = $root->appendChild($d->createElement('item'));
        $item->setAttribute('id', '1');
        $item->appendChild($d->createTextNode("a & b < \u{00FC}"));
        echo $d->saveXML();
        break;
    case 'xxe':
        // External entities are not loaded unless LIBXML_NOENT/DTDLOAD is passed.
        $xml = '<?xml version="1.0"?><!DOCTYPE r [<!ENTITY e SYSTEM "file:///etc/hostname">]><r>&e;</r>';
        $d = new DOMDocument();
        @$d->loadXML($xml);
        echo '[', $d->documentElement->textContent, ']';
        break;
    case 'simplexml':
        $x = simplexml_load_string('<r><item id="7">seven</item><item id="8">eight</item></r>');
        echo count($x->item), ',', (string) $x->item[1], ',', (string) $x->item[0]['id'];
        break;
    case 'xmlwriter':
        $w = new XMLWriter();
        $w->openMemory();
        $w->startDocument('1.0', 'UTF-8');
        $w->startElement('root');
        $w->startElement('item');
        $w->writeAttribute('id', '1');
        $w->text('a & b');
        $w->endElement();
        $w->endElement();
        $w->endDocument();
        echo $w->outputMemory();
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
