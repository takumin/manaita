package metadata

var (
	appName    string = "manaita"
	appDesc    string = "Deploy mitamae recipes to hosts over ssh"
	authorName string = "Takumi Takahashi"
)

func AppName() string {
	return appName
}

func AppDesc() string {
	return appDesc
}

func AuthorName() string {
	return authorName
}
