function escapeText(text) {
    gsub(/\&/, "\\\&amp;", text)
    gsub(/</, "\\\&lt;", text)
    gsub(/>/, "\\\&gt;", text)
    return text
}
BEGIN {
    museums[1] = "cleveland"
    museums[2] = "getty"
    for (i = 1; i <= 2; i++) {
        name = museums[i]
        file = name ".tsv"
        getline record < file
        close(file)
        split(record, fields, "\t")
        title[name] = escapeText(fields[1])
        credit[name] = escapeText(fields[4])
        file = name ".b64"
        getline bytes[name] < file
        close(file)
    }
}
{
    line = $0
    for (i = 1; i <= 2; i++) {
        name = museums[i]
        if (line == "{{" name "-image}}") {
            line = "<img alt=\"" title[name] "\" src=\"data:image/jpeg;base64," bytes[name] "\">"
            break
        }
        sub("{{" name "-title}}", title[name], line)
        sub("{{" name "-credit}}", credit[name], line)
    }
    print line
}
