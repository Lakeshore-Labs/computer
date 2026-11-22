BEGIN {
    print "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"1100\" height=\"604\" viewBox=\"0 0 1100 604\">"
    print "<title>Stage lighting cue sheet, rehearsal and opening night</title>"
    print "<rect width=\"1100\" height=\"604\" fill=\"#171b29\"/>"
    print "<g font-family=\"sans-serif\" fill=\"#eeecdf\"><text x=\"38\" y=\"42\" fill=\"#bfbdcb\" font-size=\"17\">STAGE MANAGEMENT / LIGHTING</text>"
    print "<text x=\"36\" y=\"110\" font-family=\"serif\" font-size=\"55\">From rehearsal to opening night</text>"
    print "<text x=\"38\" y=\"149\" fill=\"#c2c2cf\" font-size=\"18\">Cue duration in seconds. Bar color follows the lighting plan in each tagged edition.</text>"
    print "<path d=\"M38 176H1062\" stroke=\"#696e82\"/><text x=\"270\" y=\"216\" font-size=\"21\">Rehearsal</text><text x=\"690\" y=\"216\" font-size=\"21\">Opening night</text>"
}
NR > 1 {
    y=228+(NR-2)*58
    printf "<text x=\"38\" y=\"%d\" font-size=\"19\">%s</text>\n",y+23,$1
    printf "<rect class=\"cue\" x=\"270\" y=\"%d\" width=\"%d\" height=\"30\" fill=\"%s\"/><text x=\"%d\" y=\"%d\" font-size=\"17\">%d s</text>\n",y,7*$2,$4,280+7*$2,y+22,$2
    printf "<rect class=\"cue\" x=\"690\" y=\"%d\" width=\"%d\" height=\"30\" fill=\"%s\"/><text x=\"%d\" y=\"%d\" font-size=\"17\">%d s</text>\n",y,7*$3,$5,700+7*$3,y+22,$3
    first+=$2
    last+=$3
}
END {
    print "<path d=\"M38 532H1062\" stroke=\"#696e82\"/>"
    printf "<text x=\"38\" y=\"572\" font-size=\"19\">Total cue time</text><text x=\"270\" y=\"572\" font-size=\"22\">%d seconds</text><text x=\"690\" y=\"572\" font-size=\"22\">%d seconds</text></g></svg>\n",first,last
}
