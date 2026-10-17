<?php
require __DIR__ . '/../../_fixture.php';
$q = fixture('app/ext/intl');
header('Content-Type: text/plain; charset=UTF-8');

switch ($q['op'] ?? '') {
    case 'number_de':
        // php.net NumberFormatter::format example.
        $fmt = new NumberFormatter('de_DE', NumberFormatter::DECIMAL);
        var_dump($fmt->format(1234567.891234567890000));
        break;
    case 'currency_de':
        // The separator between amount and symbol depends on CLDR data.
        $fmt = new NumberFormatter('de_DE', NumberFormatter::CURRENCY);
        $out = $fmt->formatCurrency(1234.5, 'EUR');
        echo str_starts_with($out, '1.234,50') && str_ends_with($out, "\u{20AC}") ? 'structure ok' : "unexpected: $out";
        break;
    case 'date_pattern':
        $fmt = new IntlDateFormatter('en_US', IntlDateFormatter::NONE, IntlDateFormatter::NONE, 'UTC', IntlDateFormatter::GREGORIAN, "yyyy-MM-dd'T'HH:mm:ss");
        echo $fmt->format(1700000000);
        break;
    case 'date_de':
        $fmt = new IntlDateFormatter('de_DE', IntlDateFormatter::NONE, IntlDateFormatter::NONE, 'UTC', IntlDateFormatter::GREGORIAN, 'EEEE, d. MMMM y');
        echo $fmt->format(1700000000);
        break;
    case 'collator':
        // php.net Collator::sort example.
        $coll = collator_create('en_US');
        $arr = ['at', "\u{00E0}s", 'as'];
        collator_sort($coll, $arr);
        echo implode(',', $arr);
        break;
    case 'collator_de':
        $arr = ['Zebra', "\u{00C4}pfel", 'Birne'];
        $plain = $arr;
        sort($plain);
        (new Collator('de_DE'))->sort($arr);
        echo implode(',', $arr), '|', implode(',', $plain);
        break;
    case 'normalizer':
        // php.net Normalizer::normalize example.
        $char_A_ring = "\xC3\x85";
        $char_combining_ring_above = "\xCC\x8A";
        $char_1 = Normalizer::normalize($char_A_ring, Normalizer::FORM_C);
        $char_2 = Normalizer::normalize('A' . $char_combining_ring_above, Normalizer::FORM_C);
        echo urlencode($char_1);
        echo ' ';
        echo urlencode($char_2);
        break;
    case 'transliterator':
        echo transliterator_transliterate('Latin-ASCII', "Gr\u{00FC}\u{00DF}e \u{00C7}a");
        break;
    default:
        http_response_code(400);
        echo 'unknown op';
}
