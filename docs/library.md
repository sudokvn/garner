# Files in the library

garner manages one file per episode: the files it imports and the files it takes over. It leaves every other file in the library alone.

## Taking over files

When you add a show, garner looks for the show's video files before the monitor starts on it. It looks in two places:

- garner's folder for the show, such as `Harbor.Precinct.Night.Shift.2013.[tvdbid-42]`
- with Jellyfin, every folder that Jellyfin holds the show in, found by the show's TVDB ID, whatever the folder is called

garner takes over a file when its name clearly names episodes that have no file yet. FFprobe reads the file's audio languages and resolution to find the profile preference the file meets, as for a download. A file that meets none, such as English audio under the German profile, ranks below every preference, so an upgrade replaces it. Of two files for one episode, garner takes the one that meets the better preference, then the larger one.

The terminal interface lists the files that garner left alone, with the reason. To look again, for example after copying files into the library by hand, press `t` on the show's details page.

garner reaches Jellyfin's folders only through `jellyfin.paths` in `garner.toml`. Map every folder that Jellyfin's libraries use, including the folders of a library from before garner, and mount each one into garner's container at the mapped path. When no mapping covers a folder, the terminal interface names the folder, and garner looks only in its own folder.

## What taking over means for your files

A taken-over file belongs to garner from then on, like a file it imported:

- An upgrade replaces the file and deletes it, with its subtitle files.
- Deleting the show with **Delete files** deletes the file. **Keep files** leaves it in place, and adding the show again takes it over again.
- By default, the file keeps its name and folder. After a take-over, the terminal interface asks whether to rename and move the files outside garner's folder into that folder, named the way garner names files. **Keep as they are** is preselected.

Renaming moves the subtitle files along, never overwrites a file and, if you use Jellyfin, tells it about the moves. It removes the folders that the moves left empty. A folder that still holds other files, such as Jellyfin's `tvshow.nfo`, stays, and Jellyfin may still list it as a show. garner moves files without copying them, so a file on another filesystem than garner's folder stays where it is.
